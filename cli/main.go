package main

import (
	"github.com/ghetzel/byteflood"
	"github.com/ghetzel/byteflood/scanner"
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
					Name:  `address, a`,
					Usage: `The address the tracker should listen on`,
					Value: `127.0.0.1`,
				},
				cli.IntFlag{
					Name:  `port, p`,
					Usage: `The port the server should listen on`,
					Value: 6969,
				},
			},
			Action: func(c *cli.Context) {
				address := c.String(`address`)
				port := c.Int(`port`)

				server := byteflood.NewServer(address, port)

				if err := server.ListenAndServe(); err != nil {
					log.Fatal(err)
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
				cli.StringFlag{
					Name:  `pattern, p`,
					Usage: `A Perl-compatible regular expression that filenames must match to be included in the scan`,
				},
			},
			Action: func(c *cli.Context) {
				paths := c.StringSlice(`only`)

				for _, p := range paths {
					s := scanner.NewScanner(p, c.String(`pattern`))

					if err := s.Scan(); err != nil {
						log.Errorf("Failed to scan path %q: %v", p, err)
						continue
					}
				}
			},
		},
	}

	app.Run(os.Args)
}
