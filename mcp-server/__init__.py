#!/usr/bin/env python3
"""
AethCraft MCP Server Package

A Model Context Protocol server that provides access to the AethCraft knowledge management API.
"""

__version__ = "1.1.1"
__author__ = "AethCraft Team"
__description__ = "AethCraft MCP Server - Model Context Protocol server for AethCraft API"

from weknora_mcp_server import WeKnoraClient, run

__all__ = ["WeKnoraClient", "run"]
