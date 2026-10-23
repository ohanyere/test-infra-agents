#!/usr/bin/env python3
"""Bounded, credential-safe process trace for the isolated Playwright discovery probe."""

import argparse
import atexit
import os
import signal
import subprocess
import sys
import time
from pathlib import Path


def mark(message):
    print(message, flush=True)


def read(path):
    try:
        return Path(path).read_text(errors="replace")
    except OSError:
        return ""


def command_line(path):
    try:
        return path.read_bytes().replace(b"\0", b" ").decode(errors="replace")
    except OSError:
        return ""


def executable_name(proc):
    try:
        return Path(os.readlink(proc / "exe")).name
    except OSError:
        return Path(command_line(proc / "cmdline").split(" ", 1)[0]).name


def processes():
    result = {}
    for directory in Path("/proc").iterdir():
        if not directory.name.isdigit():
            continue
        raw = read(directory / "stat")
        if ") " not in raw:
            continue
        fields = raw.rsplit(") ", 1)[1].split()
        try:
            result[int(directory.name)] = (int(fields[1]), fields)
        except (IndexError, ValueError):
            continue
    return result


def descendants(root, process_table):
    found = {root}
    while True:
        children = {pid for pid, (ppid, _) in process_table.items() if ppid in found}
        expanded = found | children
        if expanded == found:
            return sorted(found)
        found = expanded


def snapshot(root, destination, elapsed):
    table = processes()
    with destination.open("a") as output:
        output.write(f"SNAPSHOT elapsed={elapsed:.1f}s root={root}\n")
        for pid in descendants(root, table):
            entry = table.get(pid)
            if entry is None:
                output.write(f"pid={pid} exited-before-snapshot\n")
                continue
            ppid, fields = entry
            proc = Path("/proc") / str(pid)
            command = command_line(proc / "cmdline")
            memory = next((line for line in read(proc / "status").splitlines() if line.startswith("VmRSS:")), "VmRSS: unavailable")
            try:
                cpu = (int(fields[11]) + int(fields[12])) / os.sysconf("SC_CLK_TCK")
            except (IndexError, ValueError):
                cpu = 0
            fd_dir = proc / "fd"
            try:
                fds = sorted(fd_dir.iterdir(), key=lambda item: int(item.name))
            except OSError:
                fds = []
            output.write(
                f"pid={pid} ppid={ppid} state={fields[0]} cpu_seconds={cpu:.2f} "
                f"{memory.strip()} wchan={read(proc / 'wchan').strip()} "
                f"fd_count={len(fds)} command={command}\n"
            )
            for fd in fds[:20]:
                try:
                    target = os.readlink(fd)
                except OSError:
                    continue
                output.write(f"  fd={fd.name} target={target}\n")
        output.flush()


def stop_group(process, signum):
    try:
        os.killpg(process.pid, signum)
    except ProcessLookupError:
        pass


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--output", required=True)
    parser.add_argument("--deadline", type=int, required=True)
    parser.add_argument("command", nargs=argparse.REMAINDER)
    args = parser.parse_args()
    command = args.command[1:] if args.command[:1] == ["--"] else args.command
    if not command or args.deadline < 3:
        parser.error("a command and deadline of at least three seconds are required")

    output = Path(args.output)
    output.mkdir(parents=True, exist_ok=True)
    env = os.environ.copy()
    # Node's report excludes environment variables; module tracing does not dump them.
    options = f"--report-on-signal --report-signal=SIGUSR2 --report-directory={output} --report-exclude-env"
    env["NODE_OPTIONS"] = f"{env.get('NODE_OPTIONS', '')} {options}".strip()
    env["NODE_DEBUG"] = "module,esm"

    with (output / "isolated-trace.stdout").open("wb") as stdout, (output / "isolated-trace.stderr").open("wb") as stderr:
        start = time.monotonic()
        process = subprocess.Popen(command, stdout=stdout, stderr=stderr, env=env, start_new_session=True)
        # The outer shell timeout may interrupt this tracer; always clear its process group.
        atexit.register(stop_group, process, signal.SIGKILL)

        def interrupted(signum, _frame):
            mark(f"TRACE INTERRUPTED signal={signum} pid={process.pid}")
            stop_group(process, signal.SIGTERM)
            stop_group(process, signal.SIGKILL)
            try:
                process.wait(timeout=2)
            except subprocess.TimeoutExpired:
                pass
            raise SystemExit(128 + signum)

        signal.signal(signal.SIGTERM, interrupted)
        mark(f"TRACE BEGIN pid={process.pid} deadline={args.deadline}s")
        report_sent = False
        next_snapshot = start
        while process.poll() is None and time.monotonic() - start < args.deadline:
            now = time.monotonic()
            elapsed = now - start
            if elapsed >= 3 and not report_sent:
                table = processes()
                candidates = descendants(process.pid, table)
                executables = {pid: executable_name(Path("/proc") / str(pid)) for pid in candidates}
                mark(f"TRACE REPORT_SCAN executables={executables} elapsed={elapsed:.1f}s")
                for pid in candidates:
                    if executables[pid].startswith("node"):
                        try:
                            os.kill(pid, signal.SIGUSR2)
                            mark(f"TRACE NODE_REPORT_SIGNAL pid={pid} elapsed={elapsed:.1f}s")
                        except ProcessLookupError:
                            pass
                report_sent = True
            if now >= next_snapshot:
                snapshot(process.pid, output / "isolated-processes.log", elapsed)
                next_snapshot = now + 2
            time.sleep(0.2)

        snapshot(process.pid, output / "isolated-processes.log", time.monotonic() - start)
        if process.poll() is None:
            mark(f"TRACE TIMEOUT pid={process.pid} elapsed={time.monotonic() - start:.1f}s")
            stop_group(process, signal.SIGTERM)
            try:
                process.wait(timeout=2)
            except subprocess.TimeoutExpired:
                stop_group(process, signal.SIGKILL)
                process.wait()
            mark(f"TRACE TERMINATED returncode={process.returncode}")
            return 124
        mark(f"TRACE EXIT pid={process.pid} returncode={process.returncode} elapsed={time.monotonic() - start:.1f}s")
        return process.returncode


if __name__ == "__main__":
    sys.exit(main())
