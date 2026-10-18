package service

import (
	"archive/zip"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

var auditExportSlot = make(chan struct{}, 1)

// BuildAuditExport returns a completed archive; the caller must close and remove it.
func BuildAuditExport(parent context.Context, kind, client string) (*os.File, error) {
	if kind != "sites" && kind != "client" && kind != "database" {
		return nil, fmt.Errorf("invalid audit export kind %q", kind)
	}
	if kind == "client" && (strings.TrimSpace(client) == "" || len(client) > 256) {
		return nil, fmt.Errorf("client export requires a client identifier of 1..256 characters")
	}
	select {
	case auditExportSlot <- struct{}{}:
		defer func() { <-auditExportSlot }()
	default:
		return nil, fmt.Errorf("another audit export is running; retry after it finishes")
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Minute)
	defer cancel()
	file, err := os.CreateTemp("", "xui-audit-*.zip")
	if err != nil {
		return nil, fmt.Errorf("create audit export file: %w", err)
	}
	complete := false
	defer func() {
		if !complete {
			_ = file.Close()
			_ = os.Remove(file.Name())
		}
	}()
	archive := zip.NewWriter(file)
	now := time.Now().UTC()
	counts := map[string]int64{}
	err = auditReadSnapshotContext(ctx, func(tx *gorm.DB) error {
		if isAuditChild(tx) {
			return fmt.Errorf("audit data is centralized; export from the master panel")
		}
		control, err := readAuditControl(tx)
		if err != nil {
			return err
		}
		policy := retentionFromControl(control)
		if kind == "database" {
			for _, table := range []struct {
				name  string
				model any
			}{
				{"audit_events", &model.AuditEvent{}},
				{"audit_daily_stats", &model.AuditDailyStat{}},
				{"audit_minute_stats", &model.AuditMinuteStat{}},
				// FORK(audit-bandwidth): Complete audit archives include sampled bandwidth history.
				{"audit_bandwidth_minutes", &model.AuditBandwidthMinute{}},
				{"audit_receipts", &model.AuditReceipt{}},
			} {
				count, err := auditExportJSON(archive, table.name+".jsonl", tx.Model(table.model))
				if err != nil {
					return fmt.Errorf("export %s: %w", table.name, err)
				}
				counts[table.name+".jsonl"] = count
			}
		} else {
			filter := AuditFilter{Hours: policy.SummaryDays * 24}
			if kind == "client" {
				filter.Client = client
			}
			query := auditSiteRanking(auditMetricsQuery(tx, filter, policy, now))
			count, err := auditExportSites(archive, query)
			if err != nil {
				return fmt.Errorf("export website traffic: %w", err)
			}
			counts["sites.csv"] = count
		}
		writer, err := archive.Create("manifest.json")
		if err != nil {
			return fmt.Errorf("create export manifest: %w", err)
		}
		scopeClient := ""
		if kind == "client" {
			scopeClient = client
		}
		return json.NewEncoder(writer).Encode(map[string]any{
			"version": 1, "exportedAt": now, "kind": kind, "client": scopeClient,
			"retention": policy, "rows": counts, "trafficUnit": "bytes", "timezone": "UTC",
			"notes": "Website totals combine retained details and archived summaries. Raw JSONL files preserve audit table columns; this is an audit export, not a full panel database backup.",
		})
	})
	if err != nil {
		return nil, fmt.Errorf("build audit export: %w", err)
	}
	if err = archive.Close(); err != nil {
		return nil, fmt.Errorf("finalize audit ZIP: %w", err)
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("rewind audit ZIP: %w", err)
	}
	complete = true
	return file, nil
}

func auditExportSites(archive *zip.Writer, query *gorm.DB) (int64, error) {
	writer, err := archive.Create("sites.csv")
	if err != nil {
		return 0, err
	}
	if _, err = io.WriteString(writer, "\xef\xbb\xbf"); err != nil {
		return 0, err
	}
	csvWriter := csv.NewWriter(writer)
	if err = csvWriter.Write([]string{"website", "connections", "upload_bytes", "download_bytes", "total_bytes", "measured_connections"}); err != nil {
		return 0, err
	}
	rows, err := query.Rows()
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var count int64
	for rows.Next() {
		var item AuditAggregate
		if err = query.ScanRows(rows, &item); err != nil {
			return count, err
		}
		text := item.Name
		if strings.ContainsAny(text[:min(1, len(text))], "=+-@\t\r\n") {
			text = "'" + text
		}
		values := []string{text}
		for _, number := range []int64{item.Connections, item.Uplink, item.Downlink, item.Uplink + item.Downlink, item.Measured} {
			values = append(values, strconv.FormatInt(number, 10))
		}
		if err = csvWriter.Write(values); err != nil {
			return count, err
		}
		count++
	}
	csvWriter.Flush()
	if err = rows.Err(); err != nil {
		return count, err
	}
	return count, csvWriter.Error()
}

func auditExportJSON(archive *zip.Writer, name string, query *gorm.DB) (int64, error) {
	writer, err := archive.Create(name)
	if err != nil {
		return 0, err
	}
	rows, err := query.Rows()
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return 0, err
	}
	encoder := json.NewEncoder(writer)
	var count int64
	for rows.Next() {
		values := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err = rows.Scan(pointers...); err != nil {
			return count, err
		}
		row := make(map[string]any, len(columns))
		for i, column := range columns {
			if raw, ok := values[i].([]byte); ok {
				row[column] = string(raw)
			} else {
				row[column] = values[i]
			}
		}
		if err = encoder.Encode(row); err != nil {
			return count, err
		}
		count++
	}
	return count, rows.Err()
}
