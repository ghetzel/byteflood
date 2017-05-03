package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"text/tabwriter"

	"github.com/ghetzel/byteflood/client"
	"github.com/ghetzel/byteflood/db"
	"github.com/ghetzel/byteflood/shares"
	"github.com/ghetzel/cli"
	"github.com/ghetzel/go-stockutil/sliceutil"
)

func subcommandsShares() []cli.Command {
	return []cli.Command{
		{
			Name:  `shares`,
			Usage: `Manage shared files`,
			Subcommands: []cli.Command{
				{
					Name:      `show`,
					ArgsUsage: `[NAME ..]`,
					Usage:     `List shares.`,
					Action: func(c *cli.Context) {
						var shares []shares.Share

						if err := db.Shares.All(&shares); err == nil {
							printWithFormat(c.GlobalString(`format`), shares, func() {
								tw := tabwriter.NewWriter(os.Stdout, 0, 0, 4, ' ', 0)

								fmt.Fprintf(tw, "ID\tTITLE\tFILTER\tICON\n")

								shareFilter := c.Args()
								printed := 0

								for _, share := range shares {
									if len(shareFilter) > 0 {
										if !sliceutil.ContainsString(shareFilter, fmt.Sprintf("%v", share.ID)) {
											continue
										}
									}

									fmt.Fprintf(
										tw,
										"%s\t%s\t%s\t%s\n",
										share.ID,
										share.Description,
										share.BaseFilter,
										share.IconName,
									)

									printed += 1
								}

								if len(shareFilter) > 0 && printed == 0 {
									log.Fatalf("None of the listed shares were found.")
								}

								tw.Flush()
							})
						} else {
							log.Fatal(err)
						}
					},
				}, {
					Name:      `create`,
					Usage:     `Create a share to expose files to peers.`,
					ArgsUsage: `ID [DIRECTORY]`,
					Flags: []cli.Flag{
						cli.StringFlag{
							Name:  `base-filter, F`,
							Usage: `A custom database query to use for selecting which files are exposed in this share.`,
						},
						cli.StringFlag{
							Name:  `title, t`,
							Usage: `A human-readable title for the share.`,
						},
						cli.StringFlag{
							Name:  `icon, i`,
							Usage: `A fontawesome.io icon name (excluding the "fa-" prefix) to represent the share`,
							Value: `folder`,
						},
					},
					Action: func(c *cli.Context) {
						var longDesc string
						baseFilter := c.String(`base-filter`)

						id := c.Args().First()

						if stat, _ := os.Stdin.Stat(); (stat.Mode() & os.ModeCharDevice) == 0 {
							if data, err := ioutil.ReadAll(os.Stdin); err == nil {
								longDesc = string(data[:])
							} else {
								log.Fatal(err)
							}
						}

						if err := api.CreateShare(shares.Share{
							ID:                   id,
							BaseFilter:           baseFilter,
							Description:          c.String(`title`),
							IconName:             c.String(`icon`),
							LongDescription:      longDesc,
							ScannedDirectoryPath: c.Args().Get(1),
						}); err == nil {
							log.Noticef("Share %q created successfully.", id)
						} else {
							log.Fatalf("Failed to create share %q: %v", id, err)
						}
					},
				}, {
					Name:      `remove`,
					ArgsUsage: `ID`,
					Usage:     `Remove a share.`,
					Action: func(c *cli.Context) {
						id := c.Args().Get(0)

						if id == `` {
							log.Fatalf("Must specify a share ID to remove.")
						}

						if err := api.RemoveShare(id); err == nil {
							if err := api.Cleanup(); err == nil {
								log.Noticef("Share %q has been removed.", id)
							} else {
								log.Warningf("Share %q has been removed, but the cleanup request failed.", id)
							}
						} else if client.IsNotFound(err) {
							log.Warningf("Share ID %q does not exist.", id)
						} else {
							log.Fatalf("Failed to remove share ID %q: v", id, err)
						}
					},
				},
			},
		},
	}
}
