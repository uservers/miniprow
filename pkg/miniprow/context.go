package miniprow

import (
	"os"
	"strconv"
)

type (
	ContextKey  string
	ContextData map[string]string
)

var ckey ContextKey = "data"

func NewContextData() ContextData {
	return ContextData{
		"event":   os.Getenv("MINIPROW_EVENT"),
		"repo":    os.Getenv("MINIPROW_REPO"),
		"comment": os.Getenv("MINIPROW_COMMENT"),
		"issue":   os.Getenv("MINIPROW_ISSUE"),
		"pr":      os.Getenv("MINIPROW_PR"),
		"token":   os.Getenv("MINIPROW_TOKEN"),
	}
}

func (d ContextData) getStringVal(key string) string {
	if val, ok := d[key]; ok {
		return val
	}
	return ""
}

func (d ContextData) CommentID() int64 {
	if _, ok := d["comment"]; !ok {
		return 0
	}
	commentid, err := strconv.Atoi(d["comment"])
	if err != nil {
		return 0
	}

	return int64(commentid)
}

func (d ContextData) Issue() int {
	if _, ok := d["issue"]; !ok {
		return 0
	}
	issue, err := strconv.Atoi(d["issue"])
	if err != nil {
		return 0
	}

	return issue
}

func (d ContextData) PullRequest() int {
	if _, ok := d["pr"]; !ok {
		return 0
	}
	pr, err := strconv.Atoi(d["pr"])
	if err != nil {
		return 0
	}

	return pr
}

func (d ContextData) GitHubToken() string {
	return d.getStringVal("token")
}

func (d ContextData) Repository() string {
	return d.getStringVal("repo")
}

func (d ContextData) Event() string {
	return d.getStringVal("event")
}
