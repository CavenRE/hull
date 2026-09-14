package watch

import "os"

// readDirNames returns the names of a directory's subdirectories. It reads
// entries rather than stat-ing each one, which matters because this runs over
// whole project trees.
func readDirNames(dir string) ([]string, error) {
	f, err := os.Open(dir)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	entries, err := f.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names, nil
}

// isDir reports whether path is a directory, tolerating a race where a
// just-created entry is already gone.
func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
