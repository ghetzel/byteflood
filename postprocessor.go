package byteflood

import (
	"bytes"
	"github.com/microcosm-cc/bluemonday"
	"github.com/russross/blackfriday"
	"io"
)

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

type MarkdownProcessor struct {
	Postprocessor
	writer io.Writer
	buffer bytes.Buffer
}

func NewMarkdownProcessor() *MarkdownProcessor {
	return &MarkdownProcessor{}
}

func (self *MarkdownProcessor) SetWriter(w io.Writer) {
	self.writer = w
}

func (self *MarkdownProcessor) Write(p []byte) (int, error) {
	return self.buffer.Write(p)
}

func (self *MarkdownProcessor) Flush() error {
	output := blackfriday.MarkdownCommon(self.buffer.Bytes())
	output = bluemonday.UGCPolicy().SanitizeBytes(output)

	_, err := io.Copy(self.writer, bytes.NewReader(output))
	return err
}
