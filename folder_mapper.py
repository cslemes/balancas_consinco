import re
from pathlib import Path
from typing import Optional


class FolderMapper:
    """Maps SFTP directory names to target folder names and local paths using regex."""

    def __init__(self, pattern: str = r"(?i)^bal(\d+)$"):
        self.pattern = re.compile(pattern)

    def match_folder(self, folder_name: str) -> Optional[str]:
        """Extracts target subdirectory name if folder_name matches the pattern.

        If capture groups are defined in the pattern, returns the first group
        (e.g., '01' from 'BAL01'). If no capture group is defined, returns
        the entire folder_name. Returns None if there is no match.
        """
        match = self.pattern.match(folder_name)
        if match:
            if match.groups():
                return match.group(1)
            return folder_name
        return None

    def get_local_folder(self, local_base_dir: Path, remote_folder_name: str) -> Optional[Path]:
        """Constructs the local directory Path for a given remote folder name.

        Returns None if remote_folder_name does not match the configured pattern.
        """
        sub_name = self.match_folder(remote_folder_name)
        if sub_name is not None:
            return local_base_dir / sub_name
        return None
