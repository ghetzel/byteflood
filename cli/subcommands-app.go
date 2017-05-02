package main

import (
	"fmt"

	"github.com/ghetzel/byteflood"
	"github.com/ghetzel/byteflood/encryption"
	"github.com/ghetzel/cli"
)

func subcommandsApp() []cli.Command {
	return []cli.Command{
		{
			Name:  `run`,
			Usage: `Start a file transfer peer using the given configuration`,
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:  `ui-dir`,
					Usage: `The path to the UI directory.`,
					Value: byteflood.DefaultUiDirectory,
				},
				cli.BoolFlag{
					Name:  `upnp, u`,
					Usage: `Automatically forward this port using UPnP`,
				},
				cli.IntFlag{
					Name:  `upload-limit, U`,
					Usage: `Limit uploads to this many bytes per second`,
				},
				cli.IntFlag{
					Name:  `download-limit, D`,
					Usage: `Limit downloads to this many bytes per second`,
				},
			},
			Action: func(c *cli.Context) {
				application.API.UiDirectory = c.String(`ui-dir`)
				application.LocalPeer.EnableUpnp = c.Bool(`upnp`)

				if err := application.Run(); err != nil {
					log.Fatal(err)
				}
			},
		}, {
			Name:  `id`,
			Usage: `Print your local peer ID that is shared with other peers.`,
			Action: func(c *cli.Context) {
				fmt.Printf("%v\n", application.LocalPeer.GetID())
			},
		}, {
			Name:      `genkeypair`,
			Usage:     `Generates a new public/private key pair and saves them to files in the current directory.`,
			ArgsUsage: `BASENAME`,
			Action: func(c *cli.Context) {
				if c.NArg() > 0 {
					if err := encryption.GenerateKeypair(
						fmt.Sprintf("%s.pub", c.Args().First()),
						fmt.Sprintf("%s.key", c.Args().First()),
					); err != nil {
						log.Fatalf("Failed to generate keys: %v", err)
					}
				} else {
					log.Fatalf("Must specify a base filename")
				}
			},
		},
	}
}
