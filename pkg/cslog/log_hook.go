package cslog

import (
	"bufio"
	"fmt"
	"log"
	"os"

	"github.com/sirupsen/logrus"
)

// Map for linking a log level to a log file
// Multiple levels may share a file, but multiple files may not be used for one level
type PathMap map[logrus.Level]string

// Hook to handle writing to local log files.
type lfsHook struct {
	paths   PathMap
	levels  []logrus.Level
	started bool
	send    chan string
}

// Given a map with keys equal to log levels.
// We can generate our levels handled on the fly, and write to a specific file for each level.
// We can also write to the same file for all levels. They just need to be specified.
func NewHook(levelMap PathMap) *lfsHook {
	hook := &lfsHook{
		paths: levelMap,
	}
	for level := range levelMap {
		hook.levels = append(hook.levels, level)
	}
	hook.send = make(chan string)
	return hook
}

// Open the file, write to the file, close the file.
// Whichever user is running the function needs write permissions to the file or directory if the file does not yet exist.

func (hook *lfsHook) printer(path string) {
	fd, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0666)
	if err != nil {
		log.Println("failed to open logfile:", path, err)
		return
	}
	defer fd.Close()
	w := bufio.NewWriter(fd)
	for {
		select {
		case msg := <-hook.send:
			_, err := w.WriteString(msg)
			if err != nil {
				log.Println("failed to write on the logfile:", path, err)
				return
			}
			err = w.Flush()
			if err != nil {
				log.Println("failed to flush  logfile:", path, err)
				return
			}

		}
	}
}

func (hook *lfsHook) Fire(entry *logrus.Entry) error {
	path, ok := hook.paths[entry.Level]
	if !ok {
		err := fmt.Errorf("no file provided for loglevel: %d", entry.Level)
		log.Println(err.Error())
		return err
	}

	if !hook.started {
		go hook.printer(path)
		hook.started = true
	}
	msg, err := entry.String()
	if err != nil {
		log.Println("failed to generate string for entry:", err)
		return err
	}
	hook.send <- msg
	return nil
}

func (hook *lfsHook) Levels() []logrus.Level {
	return hook.levels
}
