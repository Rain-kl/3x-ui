package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm/clause"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
)

type bandwidthCounter struct {
	ClientID, InboundID int
	CreatedAt           int64
	Client              string
	Up, Down            int64
}
type bandwidthLink struct {
	ClientID, InboundID int
	CreatedAt           int64
	Client              string
}
type bandwidthNode struct {
	At       time.Time
	From     time.Time
	Counters map[bandwidthLink]bandwidthCounter
	Rates    map[string][2]float64
}
type bandwidthKey struct {
	Minute int64
	NodeID int
	Client string
}
type bandwidthRecent struct {
	Total [2]float64
	At    time.Time
	From  time.Time
	Rates map[string][2]float64
}

type bandwidthCapture struct {
	mu       sync.Mutex
	epoch    string
	attempts map[int]time.Time
	nodes    map[int]bandwidthNode
	minutes  map[bandwidthKey]model.AuditBandwidthMinute
	recent   map[int][]bandwidthRecent
}

func newBandwidthCapture() *bandwidthCapture {
	return &bandwidthCapture{epoch: uuid.NewString(), recent: map[int][]bandwidthRecent{}, attempts: map[int]time.Time{}, nodes: map[int]bandwidthNode{}, minutes: map[bandwidthKey]model.AuditBandwidthMinute{}}
}

var auditBandwidth = newBandwidthCapture()

func captureAuditBandwidth(nodeID int) {
	now := time.Now().UTC()
	auditBandwidth.mu.Lock()
	if now.Sub(auditBandwidth.attempts[nodeID]) < 4*time.Second {
		auditBandwidth.mu.Unlock()
		return
	}
	auditBandwidth.attempts[nodeID] = now
	auditBandwidth.mu.Unlock()
	var rows []bandwidthCounter
	query := database.GetDB().Table("client_inbounds AS ci").Select("ci.client_id, ci.inbound_id, ci.created_at, clients.email AS client, ci.up, ci.down").Joins("JOIN clients ON clients.id = ci.client_id").Joins("JOIN inbounds ON inbounds.id = ci.inbound_id")
	if nodeID == 0 {
		query = query.Where("inbounds.node_id IS NULL")
	} else {
		query = query.Where("inbounds.node_id = ?", nodeID)
	}
	if err := query.Scan(&rows).Error; err != nil {
		logger.Warningf("capture audit bandwidth for node %d: %v", nodeID, err)
		return
	}
	auditBandwidth.observe(nodeID, now, rows)
}

func (s *bandwidthCapture) observe(nodeID int, now time.Time, rows []bandwidthCounter) {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous := s.nodes[nodeID]
	current := bandwidthNode{At: now, From: previous.At, Counters: make(map[bandwidthLink]bandwidthCounter, len(rows)), Rates: map[string][2]float64{}}
	elapsed := now.Sub(previous.At).Seconds()
	valid := elapsed > 0 && elapsed <= 30
	for _, row := range rows {
		key := bandwidthLink{row.ClientID, row.InboundID, row.CreatedAt, row.Client}
		current.Counters[key] = row
		old, found := previous.Counters[key]
		if !valid || !found || row.Up < old.Up || row.Down < old.Down || row.Up < 0 || row.Down < 0 {
			continue
		}
		up, down := float64(row.Up-old.Up), float64(row.Down-old.Down)
		rate := current.Rates[row.Client]
		rate[0] += up / elapsed
		rate[1] += down / elapsed
		current.Rates[row.Client] = rate
		if up > 0 || down > 0 {
			s.addInterval(nodeID, row.Client, previous.At, now, up, down, false)
		}
	}
	if valid {
		s.addInterval(nodeID, "", previous.At, now, 0, 0, true)
	}
	// FORK(audit-bandwidth): Keep actual samples across page reloads instead of reconstructing peaks from minute averages.
	if valid {
		rates := map[string][2]float64{}
		total := [2]float64{}
		for name, rate := range current.Rates {
			if rate[0] != 0 || rate[1] != 0 {
				rates[name] = rate
				total[0] += rate[0]
				total[1] += rate[1]
			}
		}
		recent := s.recent[nodeID]
		first := 0
		for first < len(recent) && recent[first].At.Before(now.Add(-time.Hour)) {
			first++
		}
		for i := 0; i < first; i++ {
			recent[i] = bandwidthRecent{}
		}
		recent = append(recent[first:], bandwidthRecent{Total: total, At: now, From: previous.At, Rates: rates})
		if len(recent) > 901 {
			recent = recent[len(recent)-901:]
		}
		s.recent[nodeID] = recent
	}
	s.nodes[nodeID] = current
}

func (s *bandwidthCapture) addInterval(nodeID int, client string, start, end time.Time, up, down float64, coverage bool) {
	duration := end.Sub(start).Seconds()
	for cursor := start; cursor.Before(end); {
		minute := cursor.Truncate(time.Minute)
		until := minute.Add(time.Minute)
		if until.After(end) {
			until = end
		}
		seconds := until.Sub(cursor).Seconds()
		key := bandwidthKey{minute.Unix(), nodeID, client}
		row := s.minutes[key]
		row.Epoch, row.Minute, row.NodeID, row.Client = s.epoch, key.Minute, nodeID, client
		row.Uplink += up * seconds / duration
		row.Downlink += down * seconds / duration
		if coverage {
			row.CoveredSeconds += seconds
		}
		s.minutes[key] = row
		cursor = until
	}
}

func FlushAuditBandwidth() {
	if err := auditBandwidth.flush(time.Now().UTC()); err != nil {
		logger.Warningf("flush audit bandwidth: %v", err)
	}
}

func (s *bandwidthCapture) flush(now time.Time) error {
	auditRetentionMu.Lock()
	defer auditRetentionMu.Unlock()
	control, err := readAuditControl(database.GetDB())
	if err != nil {
		return err
	}
	cutoff := now.Add(-24 * time.Hour).Truncate(time.Minute).Unix()
	if control.ClearBefore.Unix() > cutoff {
		cutoff = control.ClearBefore.Unix()
	}
	s.mu.Lock()
	rows := make([]model.AuditBandwidthMinute, 0, len(s.minutes))
	for key, row := range s.minutes {
		if key.Minute >= cutoff {
			rows = append(rows, row)
		} else {
			delete(s.minutes, key)
		}
	}
	for id, node := range s.nodes {
		if now.Sub(node.At) > 30*time.Minute {
			delete(s.nodes, id)
			delete(s.attempts, id)
			if len(s.recent[id]) == 0 || now.Sub(s.recent[id][len(s.recent[id])-1].At) > time.Hour {
				delete(s.recent, id)
			}
		}
	}
	for id, samples := range s.recent {
		first := 0
		for first < len(samples) && samples[first].At.Before(now.Add(-time.Hour)) {
			first++
		}
		if first == len(samples) {
			delete(s.recent, id)
		} else if first > 0 {
			s.recent[id] = append([]bandwidthRecent(nil), samples[first:]...)
		}
	}
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err = submitTrafficWrite(func() error {
		db := database.GetDB().WithContext(ctx)
		if len(rows) > 0 {
			if err := db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "epoch"}, {Name: "minute"}, {Name: "node_id"}, {Name: "client"}}, DoUpdates: clause.AssignmentColumns([]string{"uplink", "downlink", "covered_seconds"})}).CreateInBatches(rows, 200).Error; err != nil {
				return fmt.Errorf("save bandwidth minutes: %w", err)
			}
		}
		return db.Where("minute < ?", cutoff).Delete(&model.AuditBandwidthMinute{}).Error
	})
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, saved := range rows {
		key := bandwidthKey{saved.Minute, saved.NodeID, saved.Client}
		current := s.minutes[key]
		if key.Minute < now.Add(-time.Minute).Truncate(time.Minute).Unix() && current == saved {
			delete(s.minutes, key)
		}
	}
	return nil
}
