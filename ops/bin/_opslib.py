"""Shared helpers for the ops/bin checks (stdlib only).

Not a command. ticket-drift, naming-check, docs-drift and closeout-check import it.
The repo root comes from this file's location, never from the working directory.
"""
import sys

sys.dont_write_bytecode = True  # keep the working tree clean: no __pycache__ in ops/bin

import argparse
import re
import subprocess
from dataclasses import dataclass, field
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
TRACKS = ROOT / "docs" / "tracks"

ID_RE = re.compile(r"\b([A-Z]{2,5})-([FBC])-(\d{3,})\b")
RANGE_RE = re.compile(r"\b([A-Z]{2,5})-([FBC])-(\d+)\.\.(?:[A-Z]{2,5}-)?(?:[FBC]-)?(\d+)\b")
DONE_STATUS = {"done", "fixed", "shipped"}


# ---------------------------------------------------------------- reporting

@dataclass
class Finding:
    path: str
    line: int
    code: str
    msg: str
    warn: bool = False

    def render(self):
        code = ("WARN-" + self.code) if self.warn else self.code
        return f"{self.path}:{self.line}: {code}: {self.msg}"


class Report:
    def __init__(self, name):
        self.name = name
        self.items = []

    def add(self, path, line, code, msg, warn=False):
        self.items.append(Finding(rel(path), line, code, msg, warn))

    def errors(self, strict=False):
        return [f for f in self.items if strict or not f.warn]

    def finish(self, strict=False, quiet=False, extra=""):
        """Print findings (stdout) and the summary line (stderr); return the exit code."""
        seen = set()
        for f in self.items:
            text = f.render()
            if text in seen:
                continue
            seen.add(text)
            print(text)
        errs = len(self.errors(strict))
        warns = len(self.items) - errs
        if not quiet:
            tail = f" ({extra})" if extra else ""
            if errs:
                print(f"{self.name}: {errs} finding(s), {warns} warning(s){tail}", file=sys.stderr)
            else:
                print(f"{self.name}: ok, {warns} warning(s){tail}", file=sys.stderr)
        return 1 if errs else 0


def rel(path):
    p = Path(path)
    try:
        return str(p.resolve().relative_to(ROOT))
    except (ValueError, OSError):
        return str(path)


def die(msg, code=2):
    print(f"error: {msg}", file=sys.stderr)
    sys.exit(code)


# ---------------------------------------------------------------- arguments and scope

def base_parser(prog, description, epilog=""):
    p = argparse.ArgumentParser(
        prog=prog,
        description=description,
        epilog=("Exit codes: 0 pass, 1 findings (one per line as path:line: code: message), "
                "2 usage or IO error. Codes starting with WARN- do not fail the run unless --strict.\n"
                + epilog),
        formatter_class=argparse.RawDescriptionHelpFormatter,
    )
    p.add_argument("--track", help="track folder under docs/tracks/ (default: every track)")
    p.add_argument("--phase", type=int, help="phase number (default: every phase_* folder)")
    p.add_argument("--strict", action="store_true", help="count warnings as findings")
    p.add_argument("--quiet", action="store_true", help="no summary line on stderr")
    return p


def phase_dirs(track=None, phase=None):
    """Phase folders docs/tracks/<track>/phase_<N>/ in scope, sorted."""
    if not TRACKS.is_dir():
        die(f"missing {rel(TRACKS)}")
    out = []
    for t in sorted(p for p in TRACKS.iterdir() if p.is_dir()):
        if track and t.name != track:
            continue
        for ph in sorted(t.glob("phase_*")):
            if not ph.is_dir():
                continue
            m = re.fullmatch(r"phase_(\d+)", ph.name)
            if not m or (phase is not None and int(m.group(1)) != phase):
                continue
            out.append(ph)
    if (track or phase is not None) and not out:
        die(f"no phase folder matches --track {track or '*'} --phase {phase if phase is not None else '*'}")
    return out


def all_phase_dirs():
    return phase_dirs()


def phase_label(ph):
    return f"{ph.parent.name}/{ph.name}"


# ---------------------------------------------------------------- git

def git(*args, check=False):
    r = subprocess.run(["git", "-C", str(ROOT), *args], capture_output=True, text=True)
    if check and r.returncode != 0:
        die(f"git {' '.join(args)}: {r.stderr.strip()}")
    return r


def git_ok_ref(ref):
    return git("rev-parse", "--verify", "--quiet", ref + "^{commit}").returncode == 0


# ---------------------------------------------------------------- markdown tables

def split_row(line):
    parts = re.split(r"(?<!\\)\|", line.strip())
    if parts and parts[0].strip() == "":
        parts = parts[1:]
    if parts and parts[-1].strip() == "":
        parts = parts[:-1]
    return [p.strip().replace("\\|", "|") for p in parts]


@dataclass
class Table:
    h2: str
    h3: str
    header: list
    rows: list = field(default_factory=list)  # (lineno, [cells])


def parse_tables(path):
    """All pipe tables of a markdown file, each with the h2/h3 heading it sits under."""
    try:
        lines = Path(path).read_text(encoding="utf-8").splitlines()
    except OSError:
        return []
    tables, h2, h3, i = [], "", "", 0
    in_fence = False
    while i < len(lines):
        ln = lines[i]
        if ln.startswith("```"):
            in_fence = not in_fence
        if in_fence:
            i += 1
            continue
        if ln.startswith("## "):
            h2, h3 = ln[3:].strip(), ""
        elif ln.startswith("### "):
            h3 = ln[4:].strip()
        if ln.startswith("|") and i + 1 < len(lines) and re.match(r"^\|[\s:|-]+\|?\s*$", lines[i + 1]):
            t = Table(h2, h3, split_row(ln))
            j = i + 2
            while j < len(lines) and lines[j].startswith("|"):
                t.rows.append((j + 1, split_row(lines[j])))
                j += 1
            tables.append(t)
            i = j
            continue
        i += 1
    return tables


def row_dict(table, cells):
    d = {}
    for k, v in zip(table.header, cells):
        d[k.strip().lower()] = v
    return d


def ids_in(text):
    return [f"{a}-{b}-{c}" for a, b, c in ID_RE.findall(text)]


def expand_ids(cell):
    """Ticket ids in a worklog ID cell, expanding `PLT-B-007..B-012` ranges."""
    out = []
    for m in RANGE_RE.finditer(cell):
        pre, kind, lo, hi = m.group(1), m.group(2), m.group(3), m.group(4)
        w = max(3, len(lo))
        for n in range(int(lo), int(hi) + 1):
            out.append(f"{pre}-{kind}-{n:0{w}d}")
    for m in ID_RE.finditer(RANGE_RE.sub(" ", cell)):
        out.append(f"{m.group(1)}-{m.group(2)}-{m.group(3)}")
    return out


def id_key(tid):
    m = ID_RE.fullmatch(tid)
    return (m.group(1), m.group(2)), int(m.group(3))


# ---------------------------------------------------------------- tickets

@dataclass
class Ticket:
    id: str
    kind: str          # F, B or C
    phase: Path
    file: Path
    line: int
    section: str       # pending, done, shipped, chores, bugs
    raw_status: str
    state: str         # open, done, dropped
    cells: dict
    deps: list
    model: str
    pri: str
    source: str
    text: str


def _state_from_status(status):
    s = status.strip().lower()
    s = re.sub(r"[`*_~]", "", s).split()[0] if s else ""
    if "~~" in status or s == "dropped":
        return "dropped"
    if s in DONE_STATUS:
        return "done"
    return "open"


MODEL_RE = re.compile(r"Model:\s*([A-Za-z]+)")


def load_tickets(phases):
    """Tickets of backlog.md and bugs.md in the given phase folders -> (list, problems).

    problems are (file, line, code, msg) rows for malformed tables."""
    tickets, problems = [], []
    for ph in phases:
        for fname in ("backlog.md", "bugs.md"):
            f = ph / fname
            if not f.exists():
                problems.append((f, 1, "missing-file", f"{fname} not found in {phase_label(ph)}"))
                continue
            for t in parse_tables(f):
                hdr = [h.strip().lower() for h in t.header]
                if not hdr or hdr[0] != "id":
                    continue
                for ln, cells in t.rows:
                    if not cells or not ID_RE.search(cells[0]):
                        continue
                    if len(cells) != len(hdr):
                        problems.append((f, ln, "malformed-row",
                                         f"{len(cells)} cells, header has {len(hdr)} ({t.h2 or t.h3})"))
                        continue
                    d = dict(zip(hdr, cells))
                    tid = ID_RE.search(cells[0]).group(0)
                    kind = tid.split("-")[1]
                    h2 = t.h2.lower()
                    if fname == "bugs.md":
                        section, status = "bugs", d.get("status", "")
                        state = _state_from_status(status)
                    elif h2.startswith("chores"):
                        section, status = "chores", d.get("status", "")
                        state = _state_from_status(status)
                    elif h2.startswith("1."):
                        section, status = "pending", "open"
                        n = d.get("notes", "").lower()
                        status = "designed" if n.startswith("designed") else "idea"
                        state = "dropped" if "~~" in cells[0] else "open"
                    elif h2.startswith("2."):
                        section, status = "done", "done"
                        state = "dropped" if "~~" in cells[0] else "done"
                    elif h2.startswith("3."):
                        section, status = "shipped", "shipped"
                        state = "dropped" if "~~" in cells[0] else "done"
                    else:
                        problems.append((f, ln, "unknown-section", f"{tid} sits under '{t.h2}'"))
                        continue
                    deps = ids_in(d.get("depends on", ""))
                    text = " | ".join(cells)
                    model = d.get("model", "").strip()
                    if not model:
                        m = MODEL_RE.search(text)
                        model = m.group(1) if m else ""
                    tickets.append(Ticket(
                        id=tid, kind=kind, phase=ph, file=f, line=ln, section=section,
                        raw_status=status, state=state, cells=d, deps=deps, model=model,
                        pri=d.get("pri", "").strip(), source=d.get("source", "").strip(), text=text))
    return tickets, problems


def tickets_by_id(tickets):
    out = {}
    for t in tickets:
        out.setdefault(t.id, t)
    return out


# ---------------------------------------------------------------- worklog

@dataclass
class WorkRow:
    ids: list
    type: str
    status: str
    ref: str
    line: int
    file: Path
    title: str
    text: str = ""


def load_worklog(ph):
    f = ph / "worklog.md"
    rows = []
    for t in parse_tables(f):
        hdr = [h.strip().lower() for h in t.header]
        if not hdr or hdr[0] != "id":
            continue
        for ln, cells in t.rows:
            d = dict(zip(hdr, cells))
            rows.append(WorkRow(
                ids=expand_ids(cells[0]) if cells else [], type=d.get("type", "").lower(),
                status=d.get("status", "").lower(), ref=d.get("ref", ""), line=ln, file=f,
                title=d.get("title", ""), text=" | ".join(cells)))
    return rows


# ---------------------------------------------------------------- ticket sizes

SIZE_FILE = "ticket-sizes.md"


@dataclass
class SizeMap:
    path: Path
    exists: bool
    date: str = ""
    covers: dict = field(default_factory=dict)   # (prefix, kind) -> highest covered number
    sizes: dict = field(default_factory=dict)    # id -> (size, line)
    bad: list = field(default_factory=list)      # (line, code, msg)


def load_sizes(ph):
    f = ph / SIZE_FILE
    sm = SizeMap(path=f, exists=f.exists())
    if not sm.exists:
        return sm
    text = f.read_text(encoding="utf-8")
    m = re.search(r"Map date:\*{0,2}\s*(\d{4}-\d{2}-\d{2})", text)
    if m:
        sm.date = m.group(1)
    m = re.search(r"Covers ids up to:\*{0,2}\s*([^\n]+)", text)
    if m:
        for tid in ids_in(m.group(1)):
            key, n = id_key(tid)
            sm.covers[key] = max(sm.covers.get(key, 0), n)
    for t in parse_tables(f):
        hdr = [h.strip().lower() for h in t.header]
        if hdr[:3] != ["id", "size", "why"]:
            continue
        for ln, cells in t.rows:
            if len(cells) < 3 or not ID_RE.search(cells[0]):
                sm.bad.append((ln, "malformed-size-row", "expected | ID | Size | Why |"))
                continue
            tid = ID_RE.search(cells[0]).group(0)
            size = cells[1].strip().lower()
            if tid in sm.sizes:
                sm.bad.append((ln, "size-dup", f"{tid} appears twice (first at line {sm.sizes[tid][1]})"))
                continue
            if size not in ("big", "small"):
                sm.bad.append((ln, "bad-size", f"{tid} has size '{cells[1]}', expected big or small"))
            sm.sizes[tid] = (size, ln)
    return sm


# ---------------------------------------------------------------- misc

def read_lines(path):
    try:
        return Path(path).read_text(encoding="utf-8").splitlines()
    except (OSError, UnicodeDecodeError):
        return []


def md_files(roots=None):
    """README.md, ROADMAP.md, CLAUDE.md at the root plus every markdown file under docs/."""
    out = [p for p in (ROOT / "README.md", ROOT / "ROADMAP.md", ROOT / "CLAUDE.md") if p.exists()]
    out += sorted((ROOT / "docs").rglob("*.md"))
    return out
