#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
swift scripts/generate-icons.swift
iconutil -c icns build/icons/AppIcon.iconset -o build/icons/AppIcon.icns
