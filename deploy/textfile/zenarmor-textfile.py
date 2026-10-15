#!/usr/local/bin/python3
"""Optional, read-only Zenarmor daily retained-log gauges for node_exporter."""
import argparse
import datetime
import os
import signal
import stat
import sys
import tempfile
import time
from contextlib import ExitStack

PREFIX = 'opnsense_textfile_zenarmor_'
MAX_ENTRIES = 4096
MAX_LINE = 1024 * 1024
MAX_SECONDS = 10
COMPRESSED = ('.gz', '.bz2', '.xz', '.zst', '.zip')


class CollectionError(Exception):
    """The retained logs cannot provide a complete, stable daily count."""


def identity(metadata):
    """Capture mutations as well as identity; inode alone misses truncation."""
    return (metadata.st_dev, metadata.st_ino, metadata.st_mode, metadata.st_size,
            metadata.st_mtime_ns, metadata.st_ctime_ns)


def open_directory(path, dir_fd=None):
    return os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW,
                   dir_fd=dir_fd)


def collect(log_root, max_bytes, max_files, now=datetime.datetime.now):
    """Read a bounded stable inventory once; reject rotation during the scan."""
    started = time.monotonic()
    day = now()
    day_text = day.strftime('%Y-%m-%d').encode('ascii')
    day_start = day.replace(hour=0, minute=0, second=0, microsecond=0).timestamp()

    def check_time():
        if time.monotonic() - started > MAX_SECONDS:
            raise CollectionError('collection time limit exceeded')

    def inventory(directories):
        entries = 0
        selected = []
        for directory_index, descriptor in enumerate(directories):
            # scandir(fd) may share the directory offset with its duplicated fd.
            # Open dot relative to the pinned directory for an independent scan.
            with ExitStack() as scan_stack:
                scan_fd = open_directory('.', dir_fd=descriptor)
                scan_stack.callback(os.close, scan_fd)
                listing = scan_stack.enter_context(os.scandir(scan_fd))
                for entry in listing:
                    check_time()
                    entries += 1
                    if entries > MAX_ENTRIES:
                        raise CollectionError('directory entry limit exceeded')
                    if not entry.name.startswith('main_'):
                        continue
                    if entry.name.endswith(COMPRESSED):
                        raise CollectionError('compressed retained logs are unsupported')
                    metadata = entry.stat(follow_symlinks=False)
                    if not stat.S_ISREG(metadata.st_mode):
                        raise CollectionError('selected log is not a regular file')
                    selected.append((directory_index, entry.name, identity(metadata)))
                    if len(selected) > max_files:
                        raise CollectionError('matched file limit exceeded')
        if not selected:
            raise CollectionError('no retained plain main logs available')
        return sorted(selected)

    with ExitStack() as stack:
        root_fd = open_directory(log_root)
        stack.callback(os.close, root_fd)
        root_identity = identity(os.fstat(root_fd))
        directories = []
        directory_identities = []
        for name in ('active', 'archive'):
            descriptor = open_directory(name, dir_fd=root_fd)
            stack.callback(os.close, descriptor)
            directories.append(descriptor)
            directory_identities.append(identity(os.fstat(descriptor)))
        before = inventory(directories)
        seen = set()
        read_bytes = 0
        crashes = stalls = 0
        for directory_index, filename, expected in before:
            check_time()
            inode = expected[:2]
            if inode in seen:
                continue
            seen.add(inode)
            descriptor = os.open(filename, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK,
                                 dir_fd=directories[directory_index])
            with os.fdopen(descriptor, 'rb') as stream:
                metadata = os.fstat(stream.fileno())
                if not stat.S_ISREG(metadata.st_mode) or identity(metadata) != expected:
                    raise CollectionError('log changed before reading')
                if metadata.st_size > max_bytes - read_bytes:
                    raise CollectionError('total byte limit exceeded')
                while True:
                    check_time()
                    line = stream.readline(min(MAX_LINE + 1, max_bytes - read_bytes + 1))
                    if not line:
                        break
                    read_bytes += len(line)
                    if read_bytes > max_bytes:
                        raise CollectionError('total byte limit exceeded')
                    if len(line) > MAX_LINE:
                        raise CollectionError('line size limit exceeded')
                    if day_text in line:
                        crashes += int(b'terminated with signal' in line)
                        stalls += int(b'heartbeat for' in line)
                if identity(os.fstat(stream.fileno())) != expected:
                    raise CollectionError('log changed while reading')
        if now().date() != day.date():
            raise CollectionError('calendar date changed during collection')
        if inventory(directories) != before:
            raise CollectionError('log inventory changed during collection')
        for name, descriptor, expected in zip(('active', 'archive'), directories,
                                               directory_identities):
            if (identity(os.fstat(descriptor)) != expected or
                    identity(os.stat(name, dir_fd=root_fd, follow_symlinks=False)) != expected):
                raise CollectionError('log directory changed during collection')
        if (identity(os.fstat(root_fd)) != root_identity or
                identity(os.stat(log_root, follow_symlinks=False)) != root_identity):
            raise CollectionError('log root changed during collection')
        check_time()
    return crashes, stalls, day_start


def exposition(success, timestamp, counts=None):
    values = [('collection_success', 'Whether all retained plain logs were read consistently.', int(success)),
              ('collection_timestamp_seconds', 'Unix time of this collection attempt.', timestamp)]
    if success:
        crashes, stalls, day_start = counts
        values.extend([
            ('crashes_today', 'Matching crash lines for the local calendar day in retained plain logs.', crashes),
            ('stalls_today', 'Matching heartbeat stall lines for the local calendar day in retained plain logs.', stalls),
            ('day_start_timestamp_seconds', 'Unix time of local midnight for the counted day.', int(day_start)),
        ])
    return ''.join(f'# HELP {PREFIX}{name} {help_text}\n# TYPE {PREFIX}{name} gauge\n'
                   f'{PREFIX}{name} {value}\n' for name, help_text, value in values)


def publish(output_dir, content):
    """Replace the fixed destination via pinned directory FD and private temp."""
    output_fd = open_directory(output_dir)
    try:
        with tempfile.TemporaryDirectory(prefix='opn2otel-zenarmor-', dir='/var/tmp') as temporary:
            source = os.path.join(temporary, 'zenarmor.prom')
            with open(source, 'x', encoding='ascii') as stream:
                os.fchmod(stream.fileno(), 0o644)
                stream.write(content)
                stream.flush()
                os.fsync(stream.fileno())
            # Atomic replace requires /var/tmp and the output directory on one filesystem.
            # Replacing a destination symlink never follows it.
            os.replace(source, 'zenarmor.prom', dst_dir_fd=output_fd)
    finally:
        os.close(output_fd)


def positive_integer(value):
    number = int(value)
    if number <= 0:
        raise argparse.ArgumentTypeError('must be greater than zero')
    return number


def timeout_handler(_signum, _frame):
    raise CollectionError('collection time limit exceeded')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--log-root', default='/usr/local/zenarmor/log',
                        help='root containing active and archive plain main_* logs')
    parser.add_argument('--output-dir', default='/var/tmp/node_exporter',
                        help='existing node_exporter textfile directory on the /var/tmp filesystem')
    parser.add_argument('--max-bytes', type=positive_integer, default=64 * 1024 * 1024,
                        help='total bytes read, default 67108864 (64 MiB)')
    parser.add_argument('--max-files', type=positive_integer, default=128,
                        help='matched file paths, default 128')
    args = parser.parse_args()
    timestamp = int(time.time())
    counts = None
    success = False
    previous_handler = signal.signal(signal.SIGALRM, timeout_handler)
    signal.setitimer(signal.ITIMER_REAL, MAX_SECONDS)
    try:
        counts = collect(args.log_root, args.max_bytes, args.max_files)
        success = True
    except (OSError, CollectionError) as error:
        # Errors describe only the failure class; do not expose private paths/log data.
        reason = str(error) if isinstance(error, CollectionError) else type(error).__name__
        print(f'Zenarmor collection failed: {reason}', file=sys.stderr)
    finally:
        signal.setitimer(signal.ITIMER_REAL, 0)
        signal.signal(signal.SIGALRM, previous_handler)
    try:
        publish(args.output_dir, exposition(success, timestamp, counts))
    except OSError as error:
        print(f'Zenarmor publication failed: {type(error).__name__}', file=sys.stderr)
        return 1
    return 0 if success else 1


if __name__ == '__main__':
    sys.exit(main())
