package main

import (
	"io"
	"log/slog"
	"testing"
)

func TestDownCount(t *testing.T) {
	tests := []struct {
		args    []string
		want    int
		wantErr bool
	}{
		{args: []string{"down"}, want: 1},
		{args: []string{"down", "2"}, want: 2},
		{args: []string{"down", "0"}, wantErr: true},
		{args: []string{"down", "many"}, wantErr: true},
		{args: []string{"down", "1", "2"}, wantErr: true},
	}
	for _, tt := range tests {
		got, err := downCount(tt.args)
		if (err != nil) != tt.wantErr || got != tt.want {
			t.Fatalf("downCount(%v) = (%d, %v), want (%d, error=%v)", tt.args, got, err, tt.want, tt.wantErr)
		}
	}
}

func TestRunRequiresCommand(t *testing.T) {
	if err := run(nil, slog.New(slog.NewTextHandler(io.Discard, nil))); err == nil {
		t.Fatal("run() error = nil, want usage error")
	}
}
