package service

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	bolt "go.etcd.io/bbolt"

	"github.com/mhsanaei/3x-ui/v3/internal/config"
	"github.com/mhsanaei/3x-ui/v3/internal/database"
)

var bandwidthSampleBucket = []byte("samples-v1")

type storedBandwidthSample struct {
	NodeID int `json:"node"`
	bandwidthRecent
}

func bandwidthStorePath() (string, error) {
	directory := config.GetDBFolderPath()
	if !database.IsPostgres() {
		directory = ""
		var sources []struct{ Name, File string }
		if err := database.GetDB().Raw("PRAGMA database_list").Scan(&sources).Error; err != nil {
			return "", fmt.Errorf("locate bandwidth data directory: %w", err)
		}
		for _, source := range sources {
			if source.Name == "main" && source.File != "" {
				directory = filepath.Dir(source.File)
			}
		}
	}
	if directory == "" {
		return "", fmt.Errorf("bandwidth persistence requires a file-backed SQLite database")
	}
	return filepath.Join(directory, "audit-bandwidth.bbolt"), nil
}

func openBandwidthStore(path string, readOnly bool) (*bolt.DB, error) {
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("bandwidth store %s must be a regular file", path)
		}
	} else if !errors.Is(err, os.ErrNotExist) || readOnly {
		return nil, err
	}
	if !readOnly {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, err
		}
	}
	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: time.Second, ReadOnly: readOnly})
	if err != nil {
		return nil, fmt.Errorf("open bandwidth store %s: %w", path, err)
	}
	if !readOnly {
		if err := os.Chmod(path, 0o600); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("restrict bandwidth store permissions: %w", err)
		}
	}
	return db, nil
}

func validStoredBandwidth(sample bandwidthRecent, now, clearBefore time.Time) bool {
	return !sample.At.Before(now.Add(-time.Hour)) && !sample.At.After(now) && !sample.From.Before(clearBefore) && sample.At.After(sample.From) && sample.At.Sub(sample.From) <= 30*time.Second
}

func (s *bandwidthCapture) restoreRecent(now time.Time) error {
	owner := database.GetDB()
	if owner == nil {
		return fmt.Errorf("cannot restore bandwidth before database initialization")
	}
	s.mu.Lock()
	ready := s.diskOwner == owner
	s.mu.Unlock()
	if ready {
		return nil
	}
	s.diskMu.Lock()
	defer s.diskMu.Unlock()
	if s.diskOwner == owner {
		return nil
	}
	path, err := bandwidthStorePath()
	if err != nil {
		return err
	}
	control, err := readAuditControl(owner)
	if err != nil {
		return err
	}
	loaded := map[int][]bandwidthRecent{}
	saved := map[int]time.Time{}
	db, err := openBandwidthStore(path, true)
	if err == nil {
		err = db.View(func(tx *bolt.Tx) error {
			bucket := tx.Bucket(bandwidthSampleBucket)
			if bucket == nil {
				return nil
			}
			var start [16]byte
			binary.BigEndian.PutUint64(start[:8], uint64(now.Add(-time.Hour).UnixNano()))
			cursor := bucket.Cursor()
			for key, value := cursor.Seek(start[:]); key != nil; key, value = cursor.Next() {
				if len(key) != 16 {
					return fmt.Errorf("invalid persisted bandwidth sample key")
				}
				var row storedBandwidthSample
				if err := json.Unmarshal(value, &row); err != nil {
					return fmt.Errorf("decode persisted bandwidth sample: %w", err)
				}
				if validStoredBandwidth(row.bandwidthRecent, now, control.ClearBefore) {
					loaded[row.NodeID] = append(loaded[row.NodeID], row.bandwidthRecent)
					if row.At.After(saved[row.NodeID]) {
						saved[row.NodeID] = row.At
					}
				}
			}
			return nil
		})
		err = errors.Join(err, db.Close())
	} else if errors.Is(err, os.ErrNotExist) {
		err = nil
	}
	if err != nil {
		return fmt.Errorf("restore recent bandwidth: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.diskOwner != nil {
		fresh := newBandwidthCapture()
		s.epoch, s.nodes, s.attempts, s.minutes, s.recent = fresh.epoch, fresh.nodes, fresh.attempts, fresh.minutes, fresh.recent
	}
	for node, samples := range s.recent {
		loaded[node] = append(loaded[node], samples...)
	}
	for node, samples := range loaded {
		sort.SliceStable(samples, func(i, j int) bool { return samples[i].At.Before(samples[j].At) })
		kept := samples[:0]
		for _, sample := range samples {
			if !validStoredBandwidth(sample, now, control.ClearBefore) {
				continue
			}
			if len(kept) > 0 && kept[len(kept)-1].At.Equal(sample.At) {
				kept[len(kept)-1] = sample
			} else {
				kept = append(kept, sample)
			}
		}
		if len(kept) > 901 {
			kept = kept[len(kept)-901:]
		}
		loaded[node] = kept
	}
	s.recent, s.savedRecent, s.diskOwner, s.diskPath = loaded, saved, owner, path
	s.savedClearBefore = time.Time{}
	return nil
}

func (s *bandwidthCapture) persistRecent(now, clearBefore time.Time) error {
	s.diskMu.Lock()
	defer s.diskMu.Unlock()
	s.mu.Lock()
	var pending []storedBandwidthSample
	for node, samples := range s.recent {
		for _, sample := range samples {
			if sample.At.After(s.savedRecent[node]) && validStoredBandwidth(sample, now, clearBefore) {
				pending = append(pending, storedBandwidthSample{NodeID: node, bandwidthRecent: sample})
			}
		}
	}
	s.mu.Unlock()
	db, err := openBandwidthStore(s.diskPath, false)
	if err != nil {
		return err
	}
	err = db.Update(func(tx *bolt.Tx) error {
		bucket, err := tx.CreateBucketIfNotExists(bandwidthSampleBucket)
		if err != nil {
			return err
		}
		cutoff := now.Add(-time.Hour)
		if clearBefore.After(cutoff) {
			cutoff = clearBefore
		}
		cursor := bucket.Cursor()
		var expired [][]byte
		for key, _ := cursor.First(); key != nil; key, _ = cursor.Next() {
			if len(key) != 16 {
				return fmt.Errorf("invalid bandwidth sample key")
			}
			at := time.Unix(0, int64(binary.BigEndian.Uint64(key[:8])))
			if !at.Before(cutoff) {
				break
			}
			expired = append(expired, append([]byte(nil), key...))
		}
		if clearBefore.After(s.savedClearBefore) {
			var start [16]byte
			binary.BigEndian.PutUint64(start[:8], uint64(cutoff.UnixNano()))
			for key, value := cursor.Seek(start[:]); key != nil; key, value = cursor.Next() {
				if len(key) != 16 {
					return fmt.Errorf("invalid bandwidth sample key")
				}
				at := time.Unix(0, int64(binary.BigEndian.Uint64(key[:8])))
				if !at.Before(clearBefore.Add(30 * time.Second)) {
					break
				}
				var row storedBandwidthSample
				if err := json.Unmarshal(value, &row); err != nil {
					return fmt.Errorf("inspect bandwidth cleanup watermark: %w", err)
				}
				if row.From.Before(clearBefore) {
					expired = append(expired, append([]byte(nil), key...))
				}
			}
		}
		for key, _ := cursor.Last(); key != nil; key, _ = cursor.Prev() {
			if len(key) != 16 {
				return fmt.Errorf("invalid bandwidth sample key")
			}
			if !time.Unix(0, int64(binary.BigEndian.Uint64(key[:8]))).After(now) {
				break
			}
			expired = append(expired, append([]byte(nil), key...))
		}
		for _, key := range expired {
			if err := bucket.Delete(key); err != nil {
				return err
			}
		}
		for _, row := range pending {
			value, err := json.Marshal(row)
			if err != nil {
				return fmt.Errorf("encode bandwidth sample: %w", err)
			}
			var key [16]byte
			binary.BigEndian.PutUint64(key[:8], uint64(row.At.UnixNano()))
			binary.BigEndian.PutUint64(key[8:], uint64(row.NodeID))
			if err := bucket.Put(key[:], value); err != nil {
				return err
			}
		}
		return nil
	})
	err = errors.Join(err, db.Close())
	if err != nil {
		return fmt.Errorf("persist recent bandwidth: %w", err)
	}
	s.savedClearBefore = clearBefore
	for node, at := range s.savedRecent {
		if at.Before(now.Add(-time.Hour)) {
			delete(s.savedRecent, node)
		}
	}
	for _, row := range pending {
		if row.At.After(s.savedRecent[row.NodeID]) {
			s.savedRecent[row.NodeID] = row.At
		}
	}
	return nil
}

func (s *bandwidthCapture) clearRecent(now, clearBefore time.Time) error {
	if err := s.restoreRecent(now); err != nil {
		return err
	}
	s.mu.Lock()
	for node, samples := range s.recent {
		kept := samples[:0]
		for _, sample := range samples {
			if validStoredBandwidth(sample, now, clearBefore) {
				kept = append(kept, sample)
			}
		}
		s.recent[node] = kept
	}
	s.mu.Unlock()
	return s.persistRecent(now, clearBefore)
}

func (s *bandwidthCapture) recentRows(now, clearBefore time.Time) []storedBandwidthSample {
	s.mu.Lock()
	defer s.mu.Unlock()
	var rows []storedBandwidthSample
	for node, samples := range s.recent {
		for _, sample := range samples {
			if validStoredBandwidth(sample, now, clearBefore) {
				rows = append(rows, storedBandwidthSample{NodeID: node, bandwidthRecent: sample})
			}
		}
	}
	return rows
}
