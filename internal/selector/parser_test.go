package selector

import (
	"reflect"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		input    string
		expected *Selector
		wantErr  bool
	}{
		{
			input: "best",
			expected: &Selector{
				Fallbacks: []MergeGroup{
					{
						{Filters: []FormatFilter{{Type: "builtin", Value: "best"}}},
					},
				},
			},
		},
		{
			input: "bestvideo+bestaudio",
			expected: &Selector{
				Fallbacks: []MergeGroup{
					{
						{Filters: []FormatFilter{{Type: "media", Value: "video", Op: "best"}}},
						{Filters: []FormatFilter{{Type: "media", Value: "audio", Op: "best"}}},
					},
				},
			},
		},
		{
			input: "bestvideo[ext=mp4]+bestaudio[ext=m4a]",
			expected: &Selector{
				Fallbacks: []MergeGroup{
					{
						{Filters: []FormatFilter{
							{Type: "media", Value: "video", Op: "best"},
							{Type: "ext", Value: "mp4"},
						}},
						{Filters: []FormatFilter{
							{Type: "media", Value: "audio", Op: "best"},
							{Type: "ext", Value: "m4a"},
						}},
					},
				},
			},
		},
		{
			input: "bestvideo[height<=720]",
			expected: &Selector{
				Fallbacks: []MergeGroup{
					{
						{Filters: []FormatFilter{
							{Type: "media", Value: "video", Op: "best"},
							{Type: "res", Value: "720", Op: "<="},
						}},
					},
				},
			},
		},
		{
			input: "bestvideo[width>=1920]/best",
			expected: &Selector{
				Fallbacks: []MergeGroup{
					{
						{Filters: []FormatFilter{
							{Type: "media", Value: "video", Op: "best"},
							{Type: "width", Value: "1920", Op: ">="},
						}},
					},
					{
						{Filters: []FormatFilter{{Type: "builtin", Value: "best"}}},
					},
				},
			},
		},
		{
			input: "worstaudio/fps!=60",
			expected: &Selector{
				Fallbacks: []MergeGroup{
					{
						{Filters: []FormatFilter{{Type: "media", Value: "audio", Op: "worst"}}},
					},
					{
						{Filters: []FormatFilter{
							{Type: "fps", Value: "60", Op: "!="},
						}},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := Parse(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("Parse() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("Parse() = \n%#v\nwant \n%#v", got, tt.expected)
			}
		})
	}
}
