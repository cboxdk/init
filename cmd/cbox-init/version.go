package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show version information",
	Long:  `Display the version number and build information for Cbox Init`,
	Run: func(cmd *cobra.Command, args []string) {
		short, _ := cmd.Flags().GetBool("short")
		if short {
			fmt.Println(version)
		} else {
			fmt.Printf("Cbox Init v%s\n", version)
			fmt.Println("Production-grade process supervisor for Docker containers")
			fmt.Println("https://cbox.dk")
		}
	},
}

func init() {
	versionCmd.Flags().BoolP("short", "s", false, "Show only version number")
}
