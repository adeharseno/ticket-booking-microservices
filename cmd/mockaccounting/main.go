package main

import (
	"log"
	"net/http"
	"os"
	"strconv"
	"sync/atomic"
)

func main() {
	failFirstN := 2
	if v := os.Getenv("FAIL_FIRST_N"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			failFirstN = n
		}
	}

	var count int32

	http.HandleFunc("/transaction", func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&count, 1)
		key := r.Header.Get("Idempotency-Key")

		if int(n) <= failFirstN {
			log.Printf("mock accounting: request #%d (idempotency-key=%s) -> 500", n, key)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		log.Printf("mock accounting: request #%d (idempotency-key=%s) -> 200", n, key)
		w.WriteHeader(http.StatusOK)
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "9999"
	}

	log.Printf("mock accounting service listening on :%s (failing first %d requests)", port, failFirstN)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
