# Stonesthrow

This is a collection of tools for nomadic coders. I.e. people who run their
editors on machines other than the machine hosting the code.

There's probably not a whole lot here yet, but here's a quick overview.

* The tools themselves are written in `go`.
* A server instance will need to be run on each machine hosting code.
* A single repository can host multiple servers each of which can expose a
  different build configuration.
* Each build configuration will expose tools for building, running, and
  collecting test or build artifacts.

Currently capabilities are targeted at developing Chromium.
