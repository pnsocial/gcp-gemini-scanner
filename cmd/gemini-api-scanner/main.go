package main

import (
	"github.com/phuong-macair/gemini-api-scanner/internal/cli"
)

func main() {
	cli.ExitOnError(cli.Execute())
}
