package simfaas

import (
	`net/http/httptest`
	`testing`
)

func TestFission_Run(t *testing.T) {
	simFission := NewFission(0, 0)
	srv := httptest.NewServer(simFission.Serve())
	defer srv.Close()
}
