package simfaas

import (
	`net/http/httptest`
	`testing`
)

func TestFission_Setup(t *testing.T) {
	simFission := Fission{
		Platform: New(),
	}
	srv := httptest.NewServer(simFission.Serve())
	srv.Close()
}
