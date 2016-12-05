package main

import (
	"github.com/ghetzel/byteflood"
	"github.com/ghetzel/byteflood/peer"
	"github.com/ghetzel/byteflood/scanner"
	"github.com/ghetzel/cli"
	"github.com/op/go-logging"
	"os"
	"os/signal"
)

var log = logging.MustGetLogger(`main`)

func main() {
	app := cli.NewApp()
	app.Name = `byteflood`
	app.Usage = `Manages the automatic creation and serving of files via the BitTorrent protocol`
	app.Version = `0.0.1`
	app.EnableBashCompletion = false

	var config byteflood.Configuration

	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:   `log-level, L`,
			Usage:  `Level of log output verbosity`,
			Value:  `debug`,
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
		log.Infof("Loading configuration from %s", c.String(`config`))

		if cf, err := byteflood.LoadConfig(c.String(`config`)); err == nil {
			config = cf
		} else {
			log.Fatal(err)
		}

		return nil
	}

	app.Commands = []cli.Command{
		{
			Name:      `run`,
			Usage:     `Start a file transfer peer using the given configuration`,
			ArgsUsage: `PATH`,
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:  `address, a`,
					Usage: `The address the client should listen on`,
					Value: `0.0.0.0`,
				},
				cli.IntFlag{
					Name:  `port, p`,
					Usage: `The port the client should listen on`,
				},
			},
			Action: func(c *cli.Context) {
				if localPeer, err := peer.CreatePeer(``, c.String(`address`), c.Int(`port`)); err == nil {
					signalChan := make(chan os.Signal, 1)
					signal.Notify(signalChan, os.Interrupt)

					go func(p *peer.LocalPeer) {
						for _ = range signalChan {
							<-p.Close()
							break
						}

						os.Exit(0)
					}(localPeer)

					if err := localPeer.Listen(); err != nil {
						log.Fatal(err)
					}
				} else {
					log.Fatal(err)
				}
			},
		}, {
			Name:  `scan`,
			Usage: `Scans all configured source directories for changes`,
			Action: func(c *cli.Context) {
				s := scanner.NewScanner()

				if err := s.Initialize(); err == nil {
					for _, directory := range config.Directories {
						s.AddDirectory(directory.Path, directory.Options)
					}

					if err := s.Scan(); err != nil {
						log.Fatalf("Failed to scan: %v", err)
					}
				} else {
					log.Fatal(err)
				}
			},
		},
	}

	app.Run(os.Args)
}
