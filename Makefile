# Lattiam - Universal Infrastructure Management System
# Main Makefile that includes all modular components

# Default target
.DEFAULT_GOAL := help

# Include all modular makefiles in order
include mk/00-config.mk
include mk/01-build.mk
include mk/02-test.mk
include mk/03-lint.mk
include mk/99-help.mk

# Phony targets
.PHONY: all clean help