package main

import (
	"github.com/pnsocial/gemini-api-scanner/internal/cli"
)

func main() {
	cli.ExitOnError(cli.Execute())
}
