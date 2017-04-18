package stats

import (
	"github.com/alexcesaro/statsd"
	"github.com/ghetzel/go-stockutil/maputil"
	"github.com/ghetzel/go-stockutil/pathutil"
	"github.com/ghetzel/mobius"
	"github.com/op/go-logging"
	"time"
)

var log = logging.MustGetLogger(`byteflood/stats`)
var StatsdHost = `localhost:8125`
var statsdclient, _ = statsd.New()
var StatsDB *mobius.Dataset
var basetags = make(map[string]interface{})

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
			StatsDB = dataset
			log.Infof("Statistics database: %v", dataset.GetPath())

			if len(tags) > 0 {
				basetags = tags
				log.Debugf("Statistics suffix: %v", maputil.Join(basetags, `=`, mobius.InlineTagSeparator))
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
	if StatsDB != nil {
		log.Debugf("Closing statistics database")
		StatsDB.Close()
		StatsDB = nil
	}
}

func Increment(name string, tags ...map[string]interface{}) {
	IncrementN(name, 1, tags...)
}

func IncrementN(name string, count int, tags ...map[string]interface{}) {
	m := metric(name, tags)

	if StatsDB != nil {
		StatsDB.Write(m.Push(time.Now(), float64(count)))
	}

	statsdclient.Count(m.GetUniqueName(), count)
}

func Gauge(name string, value float64, tags ...map[string]interface{}) {
	m := metric(name, tags)

	if StatsDB != nil {
		StatsDB.Write(m.Push(time.Now(), value))
	}

	statsdclient.Gauge(m.GetUniqueName(), value)
}

func metric(name string, tags []map[string]interface{}) *mobius.Metric {
	outTags := basetags

	if len(tags) > 0 {
		if v, err := maputil.Merge(basetags, tags[0]); err == nil {
			outTags = v
		} else {
			panic("invalid map merge: " + err.Error())
		}
	}

	name = name + maputil.Join(outTags, `=`, mobius.InlineTagSeparator)

	return mobius.NewMetric(name)
}
