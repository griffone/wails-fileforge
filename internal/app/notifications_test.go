package app

import (
	"strings"
	"testing"
)

func TestNotifierNotifyRejectsEmptyInputs(t *testing.T) {
	notifier := &Notifier{}

	cases := []struct {
		name  string
		title string
		body  string
	}{
		{name: "empty title", title: "  ", body: "The job finished."},
		{name: "empty body", title: "Conversion done", body: " "},
		{name: "both empty", title: "", body: ""},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			err := notifier.Notify(testCase.title, testCase.body)
			if err == nil || !strings.Contains(err.Error(), "title and a body") {
				t.Fatalf("expected a title/body validation error, got %v", err)
			}
		})
	}
}

func TestNotifierNotifyWithoutServiceFails(t *testing.T) {
	notifier := &Notifier{}

	err := notifier.Notify("Conversion done", "The job finished.")
	if err == nil || !strings.Contains(err.Error(), "not available") {
		t.Fatalf("expected an unavailable-service error, got %v", err)
	}
}

func TestNotifierShutdownWithoutStartupIsSafe(t *testing.T) {
	notifier := &Notifier{}
	if err := notifier.ServiceShutdown(); err != nil {
		t.Fatalf("expected a nil shutdown error, got %v", err)
	}
}

func TestNotifyRequiresTitleAndBody(t *testing.T) {
	service := &SystemService{}

	if response := service.Notify("", "The job finished."); response.Success || response.Message == "" {
		t.Fatalf("expected an empty-title rejection with a message, got %+v", response)
	}
	if response := service.Notify("Conversion done", " "); response.Success || response.Message == "" {
		t.Fatalf("expected an empty-body rejection with a message, got %+v", response)
	}
}

func TestNotifyWithoutNotifierFails(t *testing.T) {
	service := &SystemService{}

	response := service.Notify("Conversion done", "The job finished.")
	if response.Success || response.Message == "" {
		t.Fatalf("expected a friendly failure without a notifier, got %+v", response)
	}
}
