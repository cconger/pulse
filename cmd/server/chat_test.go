package main

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestMatcher(t *testing.T) {
	for _, tc := range []struct {
		input    string
		expected *match
	}{
		{"foo", nil},
		{
			"@user -2",
			&match{
				Value: -2,
				User:  "user",
			},
		},
		{
			"@user +2",
			&match{
				Value: 2,
				User:  "user",
			},
		},
		{
			"@user +1",
			&match{
				Value: 1,
				User:  "user",
			},
		},
		{
			"@user -1",
			&match{
				Value: -1,
				User:  "user",
			},
		},
		{
			"-1 @user",
			&match{
				Value: -1,
				User:  "user",
			},
		},
		{
			"-1 #topic",
			&match{
				Value: -1,
				Topic: "topic",
			},
		},
		{
			"@user +3",
			nil,
		},
		{
			"this messsage has a +2 in it",
			&match{
				Value: 2,
			},
		},
		{
			"+2",
			&match{
				Value: 2,
			},
		},
		{
			"+2 -2",
			&match{
				Value: 2,
			},
		},
		{"long message has doesn't have + two", nil},
	} {

		out := matchMessage(tc.input)

		if !cmp.Equal(out, tc.expected) {
			t.Fail()
			t.Log(tc.input, "Did not match expected output\n", cmp.Diff(out, tc.expected))
		}
	}
}
