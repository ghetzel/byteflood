package byteflood

import (
	"fmt"
	"io"
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestQueue(t *testing.T) {
	assert := require.New(t)
	assert.NoError(generateTestDirectory())
	app := setupApplication(assert, `./tests/files`, `./tests/target`)
	ConcurrentDownloads = 10
	var completeOrder []int

	testDownloader := func(self *QueuedDownload, _ ...io.Writer) error {
		waitFor := time.Duration(3500-(self.ID*500)) * time.Millisecond
		log.Debugf("Download will take %v", waitFor)

		defer func() {
			completeOrder = append(completeOrder, self.ID)
			log.Debugf("Download Done: %v", self)
		}()

		select {
		case <-time.After(waitFor):
		case err := <-self.stopChan:
			return err
		}

		return nil
	}

	queue := NewDownloadQueue(app)
	queue.ExitOnEmpty = true
	queue.customDownloader = testDownloader

	for i := 1; i <= 5; i++ {
		download := &QueuedDownload{
			ID:              i,
			Status:          `idle`,
			SessionID:       `test`,
			PeerName:        `test`,
			ShareID:         `test`,
			FileID:          fmt.Sprintf("test_%x", i),
			Priority:        time.Now().UnixNano(),
			Name:            fmt.Sprintf("TestFile-%x", i),
			DestinationPath: `./tests/target`,
			Size:            uint64(1 + rand.Intn(1048576)),
			AddedAt:         time.Now(),
		}

		assert.NoError(queue.Enqueue(download))
	}

	queue.DownloadAll()
	log.Debugf("All downloads finished")

	assert.Equal([]int{5, 4, 3, 2, 1}, completeOrder)
}
