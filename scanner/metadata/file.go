package metadata

import (
	"os"
	"path/filepath"
)

var FileModeFlags = map[string]os.FileMode{
	`symlink`:   os.ModeSymlink,
	`device`:    os.ModeDevice,
	`pipe`:      os.ModeNamedPipe,
	`socket`:    os.ModeSocket,
	`character`: os.ModeCharDevice,
}

type FileLoader struct {
	Loader
}

func (self FileLoader) CanHandle(_ string) bool {
	return true
}

func (self FileLoader) LoadMetadata(name string) (map[string]interface{}, error) {
	if stat, err := os.Stat(name); err == nil {
		if fullPath, err := filepath.Abs(name); err == nil {
			mode := stat.Mode()
			perms := map[string]interface{}{
				`mode`:    mode.Perm(),
				`regular`: mode.IsRegular(),
			}

			for lbl, flag := range FileModeFlags {
				if (mode & flag) == flag {
					perms[lbl] = true
				}
			}

			return map[string]interface{}{
				`name`:        stat.Name(),
				`path`:        fullPath,
				`size`:        stat.Size(),
				`permissions`: perms,
				`modified_at`: stat.ModTime(),
			}, nil
		} else {
			return nil, err
		}
	} else {
		return nil, err
	}
}
