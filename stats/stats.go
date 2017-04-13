package stats

import (
	"fmt"
	"github.com/alexcesaro/statsd"
	"github.com/ghetzel/go-stockutil/maputil"
	"github.com/ghetzel/go-stockutil/pathutil"
	"github.com/ghetzel/go-stockutil/stringutil"
	"github.com/ghetzel/go-stockutil/typeutil"
	"github.com/ghetzel/mobius"
	"github.com/op/go-logging"
	"sort"
	"strings"
	"time"
)

var log = logging.MustGetLogger(`byteflood/stats`)
var StatsdHost = `localhost:8125`
var statsdclient, _ = statsd.New()
var statsdb *mobius.Dataset
var statsuffix string

func Initialize(statsdir string, tags map[string]interface{}) error {
	sdopts := make([]statsd.Option, 0)

	if StatsdHost != `` {
		sdopts = append(sdopts, statsd.Address(StatsdHost))
	} else {
		sdopts = append(sdopts, statsd.Mute(true))
	}

	if sd, err := statsd.New(sdopts...); err == nil {
		statsdclient = sd
	}

	if expandedStatsDir, err := pathutil.ExpandUser(statsdir); err == nil {
		if dataset, err := mobius.OpenDataset(expandedStatsDir); err == nil {
			statsdb = dataset
			tagkeys := maputil.StringKeys(tags)
			sort.Strings(tagkeys)

			for _, k := range tagkeys {
				if v, ok := tags[k]; ok && !typeutil.IsEmpty(v) {
					statsuffix += fmt.Sprintf(
						"%s%s=%v",
						mobius.InlineTagSeparator,
						k,
						stringutil.Autotype(v),
					)
				}
			}

			log.Infof("Statistics database: %v", dataset.GetPath())

			if statsuffix != `` {
				log.Debugf("Statistics suffix: %v", strings.TrimPrefix(statsuffix, mobius.InlineTagSeparator))
			}
		} else {
			return err
		}
	} else {
		return err
	}

	return nil
}

func Cleanup() {
	if statsdb != nil {
		log.Debugf("Closing statistics database")
		statsdb.Close()
		statsdb = nil
	}
}

func Increment(name string) {
	IncrementN(name, 1)
}

func IncrementN(name string, count int) {
	if statsdb != nil {
		statsdb.Write(mobius.NewMetric(name+statsuffix), &mobius.Point{
			Timestamp: time.Now(),
			Value:     float64(count),
		})
	}

	statsdclient.Count(name+statsuffix, count)
}

func Gauge(name string, value float64) {
	if statsdb != nil {
		statsdb.Write(mobius.NewMetric(name+statsuffix), &mobius.Point{
			Timestamp: time.Now(),
			Value:     value,
		})
	}

	statsdclient.Gauge(name+statsuffix, value)
}
