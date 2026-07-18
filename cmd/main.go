package main

import "os"

func main() {
	mode := os.Getenv("RUN_MODE") 

	switch mode {
	case "worker":

	default:
		
	}
}
