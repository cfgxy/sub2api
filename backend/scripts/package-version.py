#!/usr/bin/env python3
"""在打包入口分配一次版本标识，供镜像、二进制和交付清单共同使用。"""
import argparse
from datetime import datetime, timezone
import fcntl
import json
import os
from pathlib import Path
import re
import subprocess


ROOT = Path(__file__).resolve().parents[2]


def format_version(upstream, edition, build_date, sequence):
    upstream = upstream.removeprefix('v')
    if not re.fullmatch(r'\d+\.\d+\.\d+', upstream):
        raise ValueError('上游版本必须是不含打包后缀的 X.Y.Z')
    if edition not in ('ent', 'ob') or sequence < 1:
        raise ValueError('版本分支或打包序号无效')
    return f'{upstream}-{edition}.{build_date}-{sequence}'


def allocate_sequence(state_dir, key):
    state_dir.mkdir(parents=True, exist_ok=True)
    # 文件锁覆盖读、增量和持久化；失败的构建也消耗序号，允许有间隙。
    with (state_dir / f'{key}.counter').open('a+') as counter:
        fcntl.flock(counter, fcntl.LOCK_EX)
        counter.seek(0)
        value = int(counter.read() or '0') + 1
        counter.seek(0)
        counter.truncate()
        counter.write(str(value))
        counter.flush()
        os.fsync(counter.fileno())
        return value


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--edition', choices=('ent', 'ob'))
    parser.add_argument('--sequence', type=int)
    parser.add_argument('--state-dir', type=Path, default=Path(
        os.environ.get('XDG_STATE_HOME', str(Path.home() / '.local/state'))
    ) / 'sub2api/package-versions')
    parser.add_argument('--output', type=Path)
    parser.add_argument('--version', default='')
    parser.add_argument('--built-at', default='')
    parser.add_argument('--source-sha', default='')
    parser.add_argument('--upstream', default='')
    parser.add_argument('--require-sequence', action='store_true')
    args = parser.parse_args()
    now = (datetime.fromisoformat(args.built_at.replace('Z', '+00:00')).astimezone(timezone.utc)
           if args.built_at else datetime.now(timezone.utc))
    build_date = now.strftime('%Y%m%d')
    edition = args.edition or (ROOT / 'backend/cmd/server/EDITION').read_text().strip()
    upstream = (args.upstream or subprocess.check_output([
        'sh', str(ROOT / 'backend/scripts/resolve-version.sh')
    ], text=True).strip()).removeprefix('v')
    # 先校验输入，再消耗序号；Docker 必须复用外层打包入口分配的标识。
    format_version(upstream, edition, build_date, args.sequence or 1)
    if args.sequence is not None and args.sequence < 1:
        parser.error('打包序号必须为正整数')
    source_sha = args.source_sha or subprocess.check_output(
        ['git', '-C', str(ROOT), 'rev-parse', 'HEAD'], text=True
    ).strip()
    if not re.fullmatch(r'[0-9a-f]{40}', source_sha):
        parser.error('构建来源必须为完整 40 位 Git SHA')
    if args.version:
        match = re.fullmatch(r'(.+)-(ent|ob)\.(\d{8})-([1-9]\d*)', args.version.removeprefix('v'))
        if not match or not args.built_at:
            parser.error('复用版本时必须提供完整打包版本及原始构建时间')
        sequence = int(match[4])
        if args.sequence is not None and args.sequence != sequence:
            parser.error('打包序号与复用版本不一致')
    elif args.sequence is not None:
        sequence = args.sequence
    elif args.require_sequence:
        parser.error('请先在外层分配 VERSION 或打包序号，避免多架构重复分配')
    else:
        sequence = allocate_sequence(args.state_dir, f'{edition}-{build_date}')
    version = format_version(upstream, edition, build_date, sequence)
    if args.version and args.version.removeprefix('v') != version:
        parser.error('复用版本与上游版本、分支或 UTC 打包日期不一致')
    data = dict(version=version, upstream_version=upstream, edition=edition,
                build_date=build_date, sequence=sequence, timezone='UTC',
                built_at=now.strftime('%Y-%m-%dT%H:%M:%SZ'), source_sha=source_sha)
    if args.output:
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(json.dumps(data, ensure_ascii=False, indent=2) + '\n')
    print(version)


if __name__ == '__main__':
    main()
