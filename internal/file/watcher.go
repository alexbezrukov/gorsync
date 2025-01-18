package file

import (
	"log"

	"github.com/fsnotify/fsnotify"
)

func WatchDirectory(dir string, onChange func(string)) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer watcher.Close()

	err = watcher.Add(dir)
	if err != nil {
		return err
	}

	for {
		select {
		case event := <-watcher.Events:
			if event.Op&fsnotify.Write == fsnotify.Write {
				log.Println("Modified file:", event.Name)
				onChange(event.Name)
			}
		case err := <-watcher.Errors:
			log.Panicln("Watcher error:", err)
		}
	}
}
