#!/usr/bin/env bash
command -v tailscale >/dev/null 2>&1 && echo installed || echo not_installed
