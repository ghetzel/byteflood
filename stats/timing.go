package stats

import (
	"github.com/ghetzel/mobius"
	"time"
)

type Timing struct {
	Name      string
	StartedAt time.Time
}

func NewTiming() *Timing {
	return &Timing{
		StartedAt: time.Now(),
	}
}

func (self *Timing) Send(name string) {
	elapsed := time.Since(self.StartedAt)

	if statsdb != nil {
		statsdb.Write(mobius.NewMetric(name+statsuffix), &mobius.Point{
			Timestamp: time.Now(),
			Value:     float64(elapsed / time.Millisecond),
		})
	}

	statsdclient.Timing(name+statsuffix, elapsed)
}
