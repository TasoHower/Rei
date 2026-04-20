#!/bin/sh
# Entry for execute_shell_script (shell-only runner). Forwards argv to Python.
exec python3 "$(dirname "$0")/hello.py" "$@"
