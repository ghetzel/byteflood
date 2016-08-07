package main

import (
	"github.com/ghetzel/cli"
	"github.com/op/go-logging"
	"os"
)

var log = logging.MustGetLogger(`main`)

func main() {
	app := cli.NewApp()
	app.Name = `byteflood`
	app.Usage = `Manages the automatic creation and serving of files via the BitTorrent protocol`
	app.Version = `0.0.1`
	app.EnableBashCompletion = false

	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:   `log-level, L`,
			Usage:  `Level of log output verbosity`,
			Value:  `info`,
			EnvVar: `LOGLEVEL`,
		},
		cli.StringFlag{
			Name:  `config, c`,
			Usage: `The path to the configuration file`,
			Value: `~/.config/byteflood/config.yml`,
		},
	}

	app.Before = func(c *cli.Context) error {
		log.Infof("Starting %s %s", c.App.Name, c.App.Version)
		return nil
	}

	app.Commands = []cli.Command{
		{
			Name:  `serve`,
			Usage: `Start serving the BitTorrent tracker and management application`,
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:  `protocol, P`,
					Usage: `The protocol the tracker should use (one of: http, udp)`,
					Value: `http`,
				},
				cli.StringFlag{
					Name:  `address, a`,
					Usage: `The address the tracker should listen on`,
					Value: `127.0.0.1`,
				},
				cli.IntFlag{
					Name:  `port, p`,
					Usage: `The port the server should listen on`,
					Value: 6969,
				},
				cli.StringFlag{
					Name:  `app-address`,
					Usage: `The address the management application should listen on (if different from the tracker address)`,
					Value: `127.0.0.1`,
				},
				cli.IntFlag{
					Name:  `app-port`,
					Usage: `The port the management application should listen on (if different from the tracker port)`,
					Value: 6969,
				},
			},
			Action: func(c *cli.Context) {
				trackerAddress := c.String(`address`)
				trackerPort := c.Int(`port`)
				trackerProto := c.String(`protocol`)
				appAddress := trackerAddress
				appPort := trackerPort

				switch trackerProto {
				case `http`, `udp`:
					break
				default:
					log.Fatalf("Unrecognized tracker protocol %q", trackerProto)
					return
				}

				if v := c.String(`app-address`); v != appAddress {
					appAddress = v
				}

				if v := c.Int(`app-port`); v != appPort {
					appPort = v
				}
			},
		}, {
			Name:  `scan`,
			Usage: `Scans all configured source directories for changes and automatically manages infohash (.torrent) files`,
			Flags: []cli.Flag{
				cli.StringSliceFlag{
					Name:  `only, o`,
					Usage: `Only scan these directories, ignoring the configuration file`,
				},
			},
			Action: func(c *cli.Context) {
			},
		},
	}

	app.Run(os.Args)
}
