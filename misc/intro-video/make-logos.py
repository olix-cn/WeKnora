"""Refresh intro wordmarks using the supplied AethCraft brand variants."""
from pathlib import Path

import subprocess
import sys

here = Path(__file__).resolve().parent
root = here.parents[1]
subprocess.run([sys.executable, str(root / "scripts/sync-brand-assets.py"),
                str(root / "docs/branding/aethcraft")], check=True)
