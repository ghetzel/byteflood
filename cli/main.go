package main

import (
	"fmt"
	"github.com/ghetzel/byteflood"
	"github.com/ghetzel/byteflood/peer"
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

		if c, err := byteflood.LoadConfig(c.String(`config`)); err == nil {
			config = c
		} else {
			log.Fatal(err)
		}

		return nil
	}

	app.Commands = []cli.Command{
		{
			Name:      `run`,
			Usage:     `Starts a file transfer peer that will retrieve data from and send data to known peers.`,
			ArgsUsage: `PATH`,
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:  `address, a`,
					Usage: `The address the client should listen on`,
				},
				cli.IntFlag{
					Name:  `port, p`,
					Usage: `The port the client should listen on`,
				},
				cli.StringFlag{
					Name:  `stats-file, S`,
					Usage: `The name of a file to periodically log client stats information to`,
				},
				cli.DurationFlag{
					Name:  `import-interval, I`,
					Usage: `How often the directory should be rechecked for new torrents to seed`,
					Value: peer.DEFAULT_IMPORT_INTERVAL,
				},
			},
			Action: func(c *cli.Context) {
				if c.NArg() == 0 {
					log.Fatalf("Must specify a directory to seed.")
				}

				listenAddr := c.String(`address`)

				if port := c.Int(`port`); port != 0 {
					listenAddr = fmt.Sprintf("%s:%d", listenAddr, port)
				}

				// signalChan := make(chan os.Signal, 1)
				// signal.Notify(signalChan, os.Interrupt)

				// go func(p *peer.Peer) {
				// 	for _ = range signalChan {
				// 		<-p.Close()
				// 		break
				// 	}

				// 	os.Exit(0)
				// }(btPeer)
			},
		},
	}

	app.Run(os.Args)
}
