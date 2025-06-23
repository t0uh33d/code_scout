package models

import (
	"archive/tar"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/t0uh33d/code_scout/models/db"
	cslog "github.com/t0uh33d/code_scout/utils/cslog"
)

type logCtrl struct {
	requestID    cslog.RequestID
	loggedInUser cslog.LoggedInUser
}

func NewLogCtrl(requestCtrl cslog.RequestCtrl) *logCtrl {
	ctrl := logCtrl{}
	ctrl.requestID = requestCtrl.GetRequestID()
	ctrl.loggedInUser = requestCtrl.GetLoggedInUser()
	return &ctrl
}

func (lc *logCtrl) GetRequestID() cslog.RequestID {
	return lc.requestID
}

func (lc *logCtrl) GetLoggedInUser() cslog.LoggedInUser {
	return lc.loggedInUser
}

// CustomBool handles int to bool conversion
type CustomBool bool

// UnmarshalJSON implements custom unmarshalling for CustomBool
func (cb *CustomBool) UnmarshalJSON(data []byte) error {
	var intValue int
	if err := json.Unmarshal(data, &intValue); err != nil {
		return err
	}
	*cb = intValue != 0 // Treat 0 as false, non-zero as true
	return nil
}

type IncomingLog struct {
	ID            uuid.UUID        `json:"id"`
	SessionID     uuid.UUID        `json:"session_id"`
	Level         string           `json:"level"`
	Message       string           `json:"message"`
	Error         *string          `json:"error"`
	StackTrace    *json.RawMessage `json:"stack_trace"`
	Metadata      *json.RawMessage `json:"metadata"`
	Tags          *json.RawMessage `json:"tags"`
	Timestamp     time.Time        `json:"timestamp"`
	IsNetworkCall CustomBool       `json:"is_network_call"`
	RequestID     *uuid.UUID       `json:"request_id"`
	CallPhase     *db.CallPhase    `json:"call_phase"`
}

func (lc *logCtrl) DumpLogs(project *db.Projects, tr *tar.Reader) (int, error) {
	log := cslog.NewRequestLog(lc)
	log.Info("Dumping logs...")

	var allLogs []IncomingLog
	for {
		header, err := tr.Next()
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			log.Error(err)
			return 500, err
		}

		if header.Typeflag != tar.TypeReg {
			log.Warn("Skipping non-regular file: %s", header.Name)
			continue
		}

		var rawLogs []IncomingLog
		if err := json.NewDecoder(tr).Decode(&rawLogs); err != nil {
			log.Error(err)
			return 500, err
		}

		print(rawLogs)

		allLogs = append(allLogs, rawLogs...)
	}

	if err := lc.InsertIncomingLogs(allLogs); err != nil {
		log.Error(err)
		return 500, err
	}

	return 200, nil
}

// insert incoming logs into the database
func (lc *logCtrl) InsertIncomingLogs(logs []IncomingLog) error {
	log := cslog.NewRequestLog(lc)
	log.Info("Inserting incoming logs...")

	tx := db.GormDB.Begin()
	if tx.Error != nil {
		log.Error(tx.Error)
		return tx.Error
	}

	for _, logEntry := range logs {
		var tagArray []string
		if logEntry.Tags != nil {
			if err := json.Unmarshal(*logEntry.Tags, &tagArray); err != nil {
				log.Error(err)
				tx.Rollback()
				return err
			}
		}

		print(tagArray)

		dbLog := db.Logs{
			SessionID:     logEntry.SessionID,
			Level:         logEntry.Level,
			Message:       logEntry.Message,
			Error:         logEntry.Error,
			StackTrace:    logEntry.StackTrace,
			Metadata:      logEntry.Metadata,
			Tags:          logEntry.Tags,
			TimeStamp:     logEntry.Timestamp,
			IsNetworkCall: bool(logEntry.IsNetworkCall),
			RequestID:     logEntry.RequestID,
			CallPhase:     logEntry.CallPhase,
		}

		if err := dbLog.Create(tx); err != nil {
			log.Error(err)
			tx.Rollback()
			return err
		}
	}

	if err := tx.Commit().Error; err != nil {
		log.Error(err)
		tx.Rollback()
	}

	return nil
}
