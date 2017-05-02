package main

import (
	"github.com/ghetzel/cli"
)

func subcommandsSubscriptions() []cli.Command {
	return []cli.Command{
		{
			Name:  `subscriptions`,
			Usage: `Manage subscriptions to other peer's content.`,
			Subcommands: []cli.Command{
				{
					Name:      `show`,
					ArgsUsage: `[NAME ..]`,
					Usage:     `List subscriptions`,
					Action: func(c *cli.Context) {
					},
				}, {
					Name:      `create`,
					ArgsUsage: `SHARE TARGET SOURCES`,
					Usage:     `Create a new subscription.`,
					Action: func(c *cli.Context) {
					},
				}, {
					Name:      `delete`,
					ArgsUsage: `ID`,
					Usage:     `Remove a subscription.`,
					Action: func(c *cli.Context) {
					},
				}, {
					Name:      `sync`,
					ArgsUsage: `[NAME ..]`,
					Usage:     `Sync data from subscriptions.`,
					Action: func(c *cli.Context) {
					},
				},
			},
		},
	}
}
