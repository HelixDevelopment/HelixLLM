package tools

import (
	"context"
	"fmt"
	"time"
)

// TimeTool returns the current date and time, optionally in a specified timezone.
type TimeTool struct{}

// NewTimeTool creates a new TimeTool.
func NewTimeTool() *TimeTool {
	return &TimeTool{}
}

func (t *TimeTool) Name() string { return "time" }
func (t *TimeTool) Description() string {
	return "Returns the current date and time. Optionally accepts a 'timezone' parameter (e.g. 'UTC', 'America/New_York')."
}

func (t *TimeTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"timezone": map[string]interface{}{
			"type":        "string",
			"description": "IANA timezone name (e.g. 'UTC', 'America/New_York'). Defaults to local time.",
			"required":    false,
		},
	}
}

func (t *TimeTool) Execute(_ context.Context, args map[string]interface{}) (string, error) {
	loc := time.Now().Location()

	if args != nil {
		if tz, ok := args["timezone"]; ok {
			tzStr, isStr := tz.(string)
			if isStr && tzStr != "" {
				parsed, err := time.LoadLocation(tzStr)
				if err != nil {
					return "", fmt.Errorf("time: invalid timezone %q: %w", tzStr, err)
				}
				loc = parsed
			}
		}
	}

	now := time.Now().In(loc)
	return now.Format(time.RFC3339) + " " + loc.String(), nil
}
