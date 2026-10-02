// Command nexus is a PostgreSQL-native backend platform that lives in your terminal.
package main

import (
	"os"

	"github.com/nothikemu/nexus/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
