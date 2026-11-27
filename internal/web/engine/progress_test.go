package engine

import (
	"reflect"
	"testing"
)

func TestParseDownloadProgress(t *testing.T) {
	tests := []struct {
		name   string
		line   string
		want   Progress
		wantOK bool
	}{
		{
			name:   "full sample",
			line:   "ff-dl 1024 2048 512.5 3",
			want:   Progress{DownloadedBytes: 1024, TotalBytes: 2048, SpeedBPS: 512.5, ETA: 3},
			wantOK: true,
		},
		{
			name:   "unknown total keeps bytes and speed",
			line:   "ff-dl 900 NA 1234.5 4",
			want:   Progress{DownloadedBytes: 900, TotalBytes: 0, SpeedBPS: 1234.5, ETA: 4},
			wantOK: true,
		},
		{
			name:   "unknown speed and eta",
			line:   "ff-dl 100 200 NA NA",
			want:   Progress{DownloadedBytes: 100, TotalBytes: 200},
			wantOK: true,
		},
		{
			name:   "trailing partial line is ignored",
			line:   "ff-dl 1024 2048",
			wantOK: false,
		},
		{
			name:   "extra fields are not a progress sample",
			line:   "ff-dl 1 2 3 4 5",
			wantOK: false,
		},
		{
			name:   "other stdout is not a progress sample",
			line:   "/tmp/out/Clip [abc].mp4",
			wantOK: false,
		},
		{
			name:   "prefix without a space is not a progress sample",
			line:   "ff-dlrogress 1 2 3 4",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseDownloadProgress(tt.line)
			if ok != tt.wantOK {
				t.Fatalf("expected ok=%v, got %v (%+v)", tt.wantOK, ok, got)
			}
			if !tt.wantOK {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("unexpected sample:\n got %+v\nwant %+v", got, tt.want)
			}
		})
	}
}

func TestProgressTrackerKeepsPercentMonotonic(t *testing.T) {
	var samples []Progress
	tracker := &progressTracker{onProgress: func(sample Progress) {
		samples = append(samples, sample)
	}}

	for _, line := range []string{
		"ff-dl 500 1000 10 2",
		"ff-dl 400 1000 10 2", // out of order, must not go backwards
		"ff-dl 1000 1000 NA 0",
		"ff-dl 1200 1000 10 0", // clamped at 100
	} {
		if !tracker.handle(line) {
			t.Fatalf("expected %q to be a progress sample", line)
		}
	}

	wantPercents := []float64{50, 50, 100, 100}
	gotPercents := make([]float64, 0, len(samples))
	for _, sample := range samples {
		gotPercents = append(gotPercents, sample.Percent)
	}
	if !reflect.DeepEqual(gotPercents, wantPercents) {
		t.Fatalf("expected percents %v, got %v", wantPercents, gotPercents)
	}
}

func TestProgressTrackerKeepsUnknownTotalIndeterminate(t *testing.T) {
	var samples []Progress
	tracker := &progressTracker{onProgress: func(sample Progress) {
		samples = append(samples, sample)
	}}

	tracker.handle("ff-dl 512 NA 100 5")
	tracker.handle("ff-dl 900 NA NA NA")

	if len(samples) != 2 {
		t.Fatalf("expected two samples, got %d", len(samples))
	}
	for _, sample := range samples {
		if sample.Percent != 0 {
			t.Fatalf("an unknown total must keep Percent at 0, got %+v", sample)
		}
		if sample.TotalBytes != 0 {
			t.Fatalf("an unknown total must stay 0, got %+v", sample)
		}
	}
}
