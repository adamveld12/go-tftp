# go-tftp

![GitHub Workflow Status (main)](https://img.shields.io/github/actions/workflow/status/adamveld12/go-tftp/build.yaml?branch=main)
[![GoReportCard](https://goreportcard.com/badge/github.com/adamveld12/go-tftp)](https://goreportcard.com/report/github.com/adamveld12/go-tftp)
![GitHub](https://img.shields.io/github/license/adamveld12/go-tftp)

An impementation of TFTP, following [RFC-1350](https://www.rfc-editor.org/rfc/rfc1350)

Very WIP, written during a cup of coffee to debug a PXE booting setup

- :heavy_check_mark: `tftp get` - this works
- :facepalm: `tftp put` - this doesn't

## Dev

Open main.go and hit F5 in vscode :sunglasses:

```sh
# build it
docker build -t tftp-server .

# run it
docker run -p 6969:69 -v /tmp/tftp-server:/var/tftp-data tftp-server
```

## License

[Apache License 2.0](./LICENSE)

Copyright 2023 Adam Veldhousen
