module github.com/coulof/harvester-iso-remaster

go 1.26

replace (
	github.com/nsf/termbox-go => github.com/Harvester/termbox-go v1.1.1-0.20210318083914-8ab92204a400
	github.com/rancher/wrangler => github.com/rancher/wrangler v1.1.1
	k8s.io/api => k8s.io/api v0.32.6
	k8s.io/apimachinery => k8s.io/apimachinery v0.32.6
	k8s.io/client-go => k8s.io/client-go v0.32.6
	k8s.io/kubelet => k8s.io/kubelet v0.32.6
)

require (
	github.com/harvester/harvester-installer v1.8.2
	github.com/rancher/mapper v0.0.0-20190814232720-058a8b7feb99
	github.com/stretchr/testify v1.10.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/docker/go-units v0.5.0 // indirect
	github.com/ghodss/yaml v1.0.0 // indirect
	github.com/google/shlex v0.0.0-20191202100458-e7afc7fbc510 // indirect
	github.com/hashicorp/errwrap v1.1.0 // indirect
	github.com/hashicorp/go-multierror v1.1.1 // indirect
	github.com/imdario/mergo v0.3.16 // indirect
	github.com/itchyny/gojq v0.12.16 // indirect
	github.com/itchyny/timefmt-go v0.1.6 // indirect
	github.com/mattn/go-shellwords v1.0.10 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/rancher/wrangler v0.0.0-20190426050201-5946f0eaed19 // indirect
	github.com/rancher/yip v1.9.2 // indirect
	github.com/sirupsen/logrus v1.9.3 // indirect
	github.com/tredoe/osutil v1.5.0 // indirect
	github.com/twpayne/go-vfs/v4 v4.3.0 // indirect
	golang.org/x/sys v0.28.0 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	k8s.io/apimachinery v0.32.6 // indirect
	k8s.io/utils v0.0.0-20241104100929-3ea5e8cea738 // indirect
)
