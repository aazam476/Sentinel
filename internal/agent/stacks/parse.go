// SPDX-License-Identifier: Apache-2.0

package stacks

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/aazam476/sentinel/internal/protocol"
)

// Project is one line of `docker compose ls --format json`.
type Project struct {
	Name        string
	Containers  map[string]int
	ConfigFiles []string
}

type rawProject struct {
	Name        string `json:"Name"`
	Status      string `json:"Status"`
	ConfigFiles string `json:"ConfigFiles"`
}

// statusPart is one state in a project's status, such as "running(2)".
var statusPart = regexp.MustCompile(`^([a-z]+)\((\d+)\)$`)

// ParseProjects reads `docker compose ls --all --format json`: a JSON
// array whose Status reads like "exited(1), running(2)" and whose
// ConfigFiles are comma-separated.
func ParseProjects(output []byte) ([]Project, error) {
	var raw []rawProject
	if len(bytes.TrimSpace(output)) > 0 {
		if err := json.Unmarshal(output, &raw); err != nil {
			return nil, fmt.Errorf("read docker compose ls: %w", err)
		}
	}
	projects := make([]Project, 0, len(raw))
	for _, project := range raw {
		if checkName(project.Name) != nil {
			continue // not a name compose itself would accept; skip rather than guess
		}
		parsed := Project{Name: project.Name, Containers: map[string]int{}, ConfigFiles: []string{}}
		for part := range strings.SplitSeq(project.Status, ",") {
			if match := statusPart.FindStringSubmatch(strings.TrimSpace(part)); match != nil {
				count, err := strconv.Atoi(match[2])
				if err == nil {
					parsed.Containers[match[1]] += count
				}
			}
		}
		for file := range strings.SplitSeq(project.ConfigFiles, ",") {
			if file = strings.TrimSpace(file); file != "" {
				parsed.ConfigFiles = append(parsed.ConfigFiles, file)
			}
		}
		projects = append(projects, parsed)
	}
	return projects, nil
}

type rawService struct {
	Service string `json:"Service"`
	Name    string `json:"Name"`
	Image   string `json:"Image"`
	State   string `json:"State"`
	Status  string `json:"Status"`
	Ports   string `json:"Ports"`
}

// ParseServices reads `docker compose ps --all --format json`: one JSON
// object per line since Compose 2.21, a JSON array before.
func ParseServices(output []byte) ([]protocol.StackService, error) {
	trimmed := bytes.TrimSpace(output)
	var raw []rawService
	if bytes.HasPrefix(trimmed, []byte("[")) {
		if err := json.Unmarshal(trimmed, &raw); err != nil {
			return nil, fmt.Errorf("read docker compose ps: %w", err)
		}
	} else {
		scanner := bufio.NewScanner(bytes.NewReader(trimmed))
		scanner.Buffer(make([]byte, 64<<10), 1<<20)
		for scanner.Scan() {
			var service rawService
			if err := json.Unmarshal(scanner.Bytes(), &service); err != nil {
				return nil, fmt.Errorf("read docker compose ps: %w", err)
			}
			raw = append(raw, service)
		}
	}
	services := make([]protocol.StackService, 0, len(raw))
	for _, s := range raw {
		services = append(services, protocol.StackService{Service: s.Service, Container: s.Name, Image: s.Image,
			State: s.State, Status: s.Status, Ports: s.Ports})
	}
	return services, nil
}
