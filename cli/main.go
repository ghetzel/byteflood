package main

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"os/signal"

	"github.com/ghetzel/byteflood"
	"github.com/ghetzel/byteflood/client"
	"github.com/ghetzel/byteflood/peer"
	"github.com/ghetzel/byteflood/stats"
	"github.com/ghetzel/cli"
	"github.com/ghetzel/go-stockutil/sliceutil"
	"github.com/ghetzel/go-stockutil/stringutil"
	"github.com/ghetzel/metabase"
	"github.com/ghodss/yaml"
	"github.com/op/go-logging"
)

const DEFAULT_FORMAT = `text`

var log = logging.MustGetLogger(`main`)
var DefaultLogLevel = map[string]logging.Level{
	`run`:    logging.DEBUG,
	`export`: logging.NOTICE,
	`import`: logging.NOTICE,
	``:       logging.NOTICE,
}

var SubcommandsThatSkipTheDatabase = []string{
	`genkeypair`,
}

var application *byteflood.Application
var database *metabase.DB
var api *client.Client

func main() {
	app := cli.NewApp()
	app.Name = `byteflood`
	app.Usage = byteflood.Description
	app.Version = byteflood.Version
	app.EnableBashCompletion = true

	api = client.NewClient()

	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:   `log-level, L`,
			Usage:  `Level of log output verbosity`,
			EnvVar: `LOGLEVEL`,
		},
		cli.StringFlag{
			Name:  `peer-address, a`,
			Usage: `This is the public-facing address that should be exposed to peers.`,
			Value: peer.DEFAULT_PEER_SERVER_ADDRESS,
		},
		cli.StringFlag{
			Name:  `api-address, A`,
			Usage: `This is the private internal administrative API address.`,
			Value: byteflood.DefaultApiAddress,
		},
		cli.BoolFlag{
			Name:  `log-queries, Q`,
			Usage: `Whether to include queries in the logging output`,
		},
		cli.StringFlag{
			Name:  `config, c`,
			Usage: `The path to the configuration file`,
			Value: `~/.config/byteflood/config.yml`,
		},
		cli.StringFlag{
			Name:  `public-key, k`,
			Usage: `The path to the file containing the local public key`,
		},
		cli.StringFlag{
			Name:  `private-key, K`,
			Usage: `The path to the file containing the local private key`,
		},
		cli.StringFlag{
			Name:  `format, f`,
			Usage: `The output format to print records as (one of: json, yaml, xml, text)`,
			Value: DEFAULT_FORMAT,
		},
		cli.DurationFlag{
			Name:  `timeout, t`,
			Usage: `Byteflood client request timeout`,
			Value: client.DefaultRequestTimeout,
		},
		cli.BoolTFlag{
			Name:  `collect-stats`,
			Usage: `Whether to collect and store statistics within the application data directory.`,
		},
		cli.BoolFlag{
			Name:  `skip-db-checks`,
			Usage: `Bypass the ordinary startup verification of the database schema.`,
		},
	}

	app.Before = func(c *cli.Context) error {
		logging.SetFormatter(logging.MustStringFormatter(`%{color}%{level:.4s}%{color:reset}[%{id:04d} %{shortfile:30s}] %{message}`))

		if logLevel := c.String(`log-level`); logLevel == `` {
			if lvl, ok := DefaultLogLevel[c.Args().First()]; ok {
				logging.SetLevel(lvl, ``)
			} else if lvl, ok := DefaultLogLevel[``]; ok {
				logging.SetLevel(lvl, ``)
			} else {
				logging.SetLevel(logging.DEBUG, ``)
			}
		} else {
			if level, err := logging.LogLevel(logLevel); err == nil {
				logging.SetLevel(level, ``)
			} else {
				return err
			}
		}

		if c.Bool(`log-queries`) {
			logging.SetLevel(logging.DEBUG, `pivot/querylog`)
		} else {
			logging.SetLevel(logging.CRITICAL, `pivot/querylog`)
		}

		logging.SetLevel(logging.ERROR, `diecast`)

		api.Timeout = c.Duration(`timeout`)
		api.Address = c.String(`api-address`)
		stats.LocalStatsEnabled = c.Bool(`collect-stats`)

		log.Debugf("API address is %s", api.Address)

		log.Infof("Starting %s %s", c.App.Name, c.App.Version)

		if !sliceutil.ContainsString(SubcommandsThatSkipTheDatabase, c.Args().First()) {
			if a, err := createApplication(c); err == nil {
				application = a
				database = a.Database
				application.LocalPeer.Address = c.String(`peer-address`)
			} else {
				log.Fatal(err)
			}
		}

		return nil
	}

	app.Commands = append(app.Commands, subcommandsAPI()...)
	app.Commands = append(app.Commands, subcommandsApp()...)
	app.Commands = append(app.Commands, subcommandsDB()...)
	app.Commands = append(app.Commands, subcommandsPeers()...)
	app.Commands = append(app.Commands, subcommandsShares()...)
	app.Commands = append(app.Commands, subcommandsSubscriptions()...)

	app.Run(os.Args)
}

func createApplication(c *cli.Context) (*byteflood.Application, error) {
	log.Infof("Loading configuration from %s", c.GlobalString(`config`))

	if app, err := byteflood.NewApplicationFromConfig(c.GlobalString(`config`)); err == nil {
		if c.GlobalIsSet(`public-key`) {
			app.PublicKeyPath = c.GlobalString(`public-key`)
		}

		if c.GlobalIsSet(`private-key`) {
			app.PublicKeyPath = c.GlobalString(`private-key`)
		}

		if c.Bool(`skip-db-checks`) {
			app.Database.SkipMigrate = true
			log.Warning("Database schema integrity checks are being skipped.")
		}

		if err := app.Initialize(); err == nil {
			signalChan := make(chan os.Signal, 1)
			signal.Notify(signalChan, os.Interrupt)

			go func(a *byteflood.Application) {
				for _ = range signalChan {
					a.Stop()
					break
				}

				os.Exit(0)
			}(app)

			return app, nil
		} else {
			return nil, err
		}

	} else {
		return nil, err
	}
}

func printWithFormat(format string, data interface{}, fallbackFunc ...func()) {
	var output []byte
	var err error

	switch format {
	case `json`:
		output, err = json.MarshalIndent(data, ``, `  `)
	case `yaml`:
		output, err = yaml.Marshal(data)
	case `xml`:
		output, err = xml.MarshalIndent(data, ``, `  `)
	default:
		if len(fallbackFunc) > 0 {
			f := fallbackFunc[0]
			f()
			return
		} else if data != nil {
			if v, err := stringutil.ToString(data); err == nil {
				output = []byte(v[:])
			} else {
				log.Fatal(err)
			}
		}
	}

	if err == nil {
		fmt.Println(string(output[:]))
	} else {
		log.Fatal(err)
	}
}

func errAndMaybeQuit(c *cli.Context, format string, values ...interface{}) {
	log.Errorf(format, values...)

	if !c.Bool(`ignore-errors`) {
		os.Exit(1)
	}
}
