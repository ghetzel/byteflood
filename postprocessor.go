package byteflood

import "io"

type Postprocessor interface {
	io.Writer
	SetWriter(io.Writer)
	Flush() error
}

func GetPostprocessorByName(name string) (Postprocessor, bool) {
	switch name {
	case `markdown`:
		return NewMarkdownProcessor(), true
	}

	return nil, false
}
