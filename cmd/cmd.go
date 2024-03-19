package cmd

import (
	"fmt"
	"os"
)

func handleErr(msg interface{}) {
	fmt.Println("Error:", msg)
	os.Exit(1)
}
