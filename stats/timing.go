package stats

import (
	"github.com/ghetzel/mobius"
	"time"
)

type Timing struct {
	Name      string
	StartedAt time.Time
}

func NewTiming(times ...time.Time) *Timing {
	if len(times) == 0 {
		times = []time.Time{time.Now()}
	}

	return &Timing{
		StartedAt: times[0],
	}
}

func (self *Timing) Send(name string) {
	Elapsed(name, time.Since(self.StartedAt))
}

func Elapsed(name string, duration time.Duration) {
	if StatsDB != nil {
		StatsDB.Write(mobius.NewMetric(name+statsuffix).Push(time.Now(), float64(duration)/float64(time.Millisecond)))
	}

	statsdclient.Timing(name+statsuffix, duration)
}
