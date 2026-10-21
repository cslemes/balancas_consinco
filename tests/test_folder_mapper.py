import sys
from pathlib import Path

# Ensure project root is in sys.path
sys.path.insert(0, str(Path(__file__).resolve().parent.parent))

from folder_mapper import FolderMapper


def test_folder_mapper_matches():
    mapper = FolderMapper(pattern=r"(?i)^bal(\d+)$")
    assert mapper.match_folder("BAL01") == "01"
    assert mapper.match_folder("bal03") == "03"
    assert mapper.match_folder("BAL10") == "10"
    assert mapper.match_folder("BAL92") == "92"


def test_folder_mapper_default_pattern():
    mapper = FolderMapper()
    assert mapper.match_folder("BAL01") == "01"
    assert mapper.match_folder("bal03") == "03"


def test_folder_mapper_ignores_non_matching():
    mapper = FolderMapper(pattern=r"(?i)^bal(\d+)$")
    assert mapper.match_folder("BALANCAS") is None
    assert mapper.match_folder("blackbox.zip") is None
    assert mapper.match_folder("IMPORT") is None
    assert mapper.match_folder(".snapshot") is None
    assert mapper.match_folder("") is None


def test_folder_mapper_without_capture_group():
    mapper = FolderMapper(pattern=r"(?i)^bal\d+$")
    assert mapper.match_folder("BAL01") == "BAL01"
    assert mapper.match_folder("bal99") == "bal99"
    assert mapper.match_folder("xyz") is None



def test_folder_mapper_get_local_folder():
    mapper = FolderMapper(pattern=r"(?i)^bal(\d+)$")
    base = Path("C:/balancas")
    assert mapper.get_local_folder(base, "BAL01") == Path("C:/balancas/01")
    assert mapper.get_local_folder("C:/balancas", "BAL01") == Path("C:/balancas/01")
    assert mapper.get_local_folder(base, "BAL93") == Path("C:/balancas/93")
    assert mapper.get_local_folder(base, "BALANCAS") is None
