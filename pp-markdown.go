package byteflood

import (
	"bytes"
	"io"

	"github.com/microcosm-cc/bluemonday"
	"github.com/russross/blackfriday"
)

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
