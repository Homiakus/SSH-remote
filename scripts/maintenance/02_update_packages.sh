#!/bin/bash
echo "=== System Update ==="
apt update -y 2>/dev/null && apt upgrade -y 2>/dev/null || \
yum update -y 2>/dev/null || \
echo "Package manager not found"
echo "Done"
