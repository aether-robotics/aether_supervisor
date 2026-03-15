package types

import (
	"encoding/json"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestAppSpecJSONUnmarshal(t *testing.T) {
	input := []byte(`{
		"name": "demo",
		"services": {
			"web": {
				"image": "nginx:latest",
				"command": ["nginx", "-g", "daemon off;"],
				"environment": {
					"FOO": "bar",
					"EMPTY": null
				},
				"env_file": "service.env",
				"ports": [
					"8080:80",
					{"target": 443, "published": "8443", "protocol": "tcp"}
				],
				"volumes": [
					"./data:/data",
					{"type": "bind", "source": "/tmp", "target": "/host-tmp", "read_only": true}
				],
				"depends_on": {
					"db": {"condition": "service_healthy", "restart": true}
				},
				"networks": {
					"frontend": {"aliases": ["public-web"]}
				},
				"healthcheck": {
					"test": ["CMD", "curl", "-f", "http://localhost"],
					"interval": "30s"
				}
			}
		}
	}`)

	var spec AppSpec
	if err := json.Unmarshal(input, &spec); err != nil {
		t.Fatalf("unmarshal json: %v", err)
	}

	service := spec.Services["web"]
	if service.Image != "nginx:latest" {
		t.Fatalf("unexpected image: %s", service.Image)
	}
	if len(service.Command.Exec) != 3 {
		t.Fatalf("unexpected command exec form: %#v", service.Command.Exec)
	}
	if service.Environment.Mapping["FOO"] == nil || *service.Environment.Mapping["FOO"] != "bar" {
		t.Fatalf("unexpected environment map: %#v", service.Environment.Mapping)
	}
	if len(service.EnvFile.Values) != 1 || service.EnvFile.Values[0] != "service.env" {
		t.Fatalf("unexpected env_file values: %#v", service.EnvFile.Values)
	}
	if service.Ports[0].Short != "8080:80" {
		t.Fatalf("unexpected short port: %#v", service.Ports[0])
	}
	if service.Ports[1].Config == nil || service.Ports[1].Config.Target != 443 {
		t.Fatalf("unexpected long port: %#v", service.Ports[1].Config)
	}
	if service.Volumes[1].Config == nil || service.Volumes[1].Config.Target != "/host-tmp" {
		t.Fatalf("unexpected long volume: %#v", service.Volumes[1].Config)
	}
	if service.DependsOn.Values["db"].Condition != "service_healthy" {
		t.Fatalf("unexpected depends_on map: %#v", service.DependsOn.Values)
	}
	if len(service.Networks.Values["frontend"].Aliases) != 1 {
		t.Fatalf("unexpected networks map: %#v", service.Networks.Values)
	}
	if service.Healthcheck == nil || len(service.Healthcheck.Test.Exec) != 4 {
		t.Fatalf("unexpected healthcheck: %#v", service.Healthcheck)
	}
}

func TestAppSpecYAMLUnmarshal(t *testing.T) {
	input := []byte(`
name: demo
services:
  ur3e_controller:
    image: dkhoanguyen/robotic_base:latest
    command:
      - bash
      - -c
      - source /opt/ros/noetic/setup.bash
    environment:
      ROS_MASTER_URI: http://localhost:11311
      ROS_IP: "192.168.27.1"
    env_file:
      - common.env
      - local.env
    volumes:
      - type: bind
        source: /tmp
        target: /tmp
      - ./calibration_file:/calibration_file
    ports:
      - "8080:8080"
    depends_on:
      - ros_master
    dns: 8.8.8.8
    extra_hosts:
      - "controller.local:127.0.0.1"
    labels:
      com.example.role: controller
    healthcheck:
      test: curl -f http://localhost:8080/health
      timeout: 5s
`)

	var spec AppSpec
	if err := yaml.Unmarshal(input, &spec); err != nil {
		t.Fatalf("unmarshal yaml: %v", err)
	}

	service := spec.Services["ur3e_controller"]
	if service.Command.Shell != nil {
		t.Fatalf("expected exec-form command from yaml list, got shell form")
	}
	if len(service.EnvFile.Values) != 2 {
		t.Fatalf("unexpected env_file values: %#v", service.EnvFile.Values)
	}
	if len(service.DependsOn.Names) != 1 || service.DependsOn.Names[0] != "ros_master" {
		t.Fatalf("unexpected depends_on names: %#v", service.DependsOn.Names)
	}
	if len(service.DNS.Values) != 1 || service.DNS.Values[0] != "8.8.8.8" {
		t.Fatalf("unexpected dns values: %#v", service.DNS.Values)
	}
	if service.Healthcheck == nil || service.Healthcheck.Test.Shell == nil {
		t.Fatalf("expected shell-form healthcheck test, got %#v", service.Healthcheck)
	}
}
