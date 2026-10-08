#!/usr/bin/env python3
"""Exercise installer downloads and persistence in a temporary, unprivileged sandbox."""
import fcntl
import hashlib
import os
from pathlib import Path
import pty
import shutil
import subprocess
import tarfile
import tempfile
import termios

ROOT = Path(__file__).resolve().parent.parent
binary = ROOT / 'dist/gatehouse'
if not binary.is_file():
    raise SystemExit('Run make build before installer checks.')

with tempfile.TemporaryDirectory(prefix='gatehome-install-checks.') as temporary:
    temp = Path(temporary)
    package, assets, prefix, legacy = [temp / name for name in ('package', 'assets', 'gatehome', 'legacy')]
    for directory in (package, assets, legacy, temp / 'systemd', temp / 'tmp'):
        directory.mkdir(mode=0o700)
    (package / 'dist').mkdir()
    (package / 'deploy').mkdir()
    shutil.copy2(ROOT / 'install.sh', package / 'install.sh')
    shutil.copy2(ROOT / 'deploy/gatehouse.service', package / 'deploy/gatehouse.service')
    shutil.copy2(ROOT / 'VERSION', package / 'VERSION')
    checksums = []
    for arch in ('amd64', 'arm64'):
        name = 'dist/gatehouse-linux-' + arch
        shutil.copy2(binary, package / name)
        checksums.append(hashlib.sha256(binary.read_bytes()).hexdigest() + '  ' + name)
    (package / 'SHA256SUMS').write_text('\n'.join(checksums) + '\n')
    for arch in ('amd64', 'arm64'):
        archive = assets / ('gatehome-linux-' + arch + '.tar.gz')
        with tarfile.open(archive, 'w:gz') as tar:
            for file in package.rglob('*'):
                if file.is_file(): tar.add(file, arcname=file.relative_to(package).as_posix())
    (assets / 'gatehome-SHA256SUMS').write_text(''.join(hashlib.sha256(file.read_bytes()).hexdigest() + '  ' + file.name + '\n' for file in assets.glob('*.tar.gz')))
    fake_curl = temp / 'curl'
    fake_curl.write_text('''#!/usr/bin/env python3
import os,sys,shutil
from pathlib import Path
args=sys.argv[1:]
expected=os.environ.get('QA_EXPECT_PROXY')
if expected is not None:
    if '--proxy' not in args or args[args.index('--proxy')+1] != expected:
        sys.exit(1)
    if '--noproxy' not in args or args[args.index('--noproxy')+1] != '':
        sys.exit(1)
url=next(a for a in args if a.startswith('https://'))
output=args[args.index('-o')+1]
name=url.rsplit('/',1)[-1]
if name.endswith('.tar.gz'):
    counter=Path(os.environ['QA_CURL_COUNTER'])
    count=int(counter.read_text())+1 if counter.exists() else 1
    counter.write_text(str(count))
    if count <= int(os.environ.get('QA_DOWNLOAD_FAILURES','0')):
        Path(output).write_bytes(b'TEST_ONLY_PARTIAL_DOWNLOAD')
        sys.exit(56)
shutil.copy2(Path(os.environ['QA_ASSETS'])/name,output)
''')
    fake_curl.chmod(0o755)
    environment = os.environ | {'QA_PREFIX': str(prefix), 'QA_LEGACY': str(legacy), 'QA_PACKAGE': str(package), 'QA_ASSETS': str(assets), 'QA_SYSTEMD': str(temp / 'systemd'), 'QA_CURL_COUNTER': str(temp / 'curl-attempts'), 'TMPDIR': str(temp / 'tmp'), 'PATH': str(temp) + os.pathsep + os.environ['PATH']}
    # Privilege and systemd commands are the only mocked installation operations.
    shell = '''source "$QA_PACKAGE/install.sh"
INSTALL_DIR="$QA_PREFIX"; CONFIG_DIR="$INSTALL_DIR/config"; LOG_DIR="$INSTALL_DIR/log"; DATA_DIR="$INSTALL_DIR/data"
LEGACY_DATA_DIR="$QA_LEGACY"; SERVICE_FILE="$QA_SYSTEMD/gatehouse.service"
require_linux() { :; }
ensure_ca_certificates() { :; }
uname() { case "$1" in -m) printf x86_64;; -s) printf Linux;; esac; }
getent() { return 0; }
chown() { :; }
hostname() { :; }
install() {
  local args=()
  while [[ "$#" -gt 0 ]]; do
    case "$1" in -o|-g) shift 2;; *) args+=("$1"); shift;; esac
  done
  command install "${args[@]}"
}
runuser() { shift 3; "$@"; }
systemctl() {
  if [[ "$1" == enable ]]; then
    "$INSTALL_DIR/app/gatehouse" -config "$CONFIG_DIR" -log "$LOG_DIR" -data "$DATA_DIR" migrate
    mkdir -p "$DATA_DIR/maintenance"
    printf '{"pid":1}' > "$DATA_DIR/maintenance/ready.json"
  fi
}
trap '[[ -z "$DOWNLOAD_DIR" ]] || rm -rf -- "$DOWNLOAD_DIR"' EXIT
'''
    def execute(action, input_text='', success=True):
        master, slave = pty.openpty()
        attributes = termios.tcgetattr(slave)
        attributes[3] &= ~termios.ECHO
        termios.tcsetattr(slave, termios.TCSANOW, attributes)
        def controlling_terminal():
            os.setsid()
            fcntl.ioctl(0, termios.TIOCSCTTY, 0)
        process = subprocess.Popen(['bash', '-c', shell + action], stdin=slave, stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=environment, preexec_fn=controlling_terminal)
        os.close(slave)
        if input_text: os.write(master, input_text.encode())
        try:
            out, err = process.communicate(timeout=20)
        finally:
            os.close(master)
        if (process.returncode == 0) != success:
            raise AssertionError('Installer check failed; exit code ' + str(process.returncode))
        return out
    def snapshot():
        return {file.relative_to(prefix).as_posix(): file.read_bytes() for folder in ('config', 'log', 'data') for file in (prefix / folder).rglob('*') if file.is_file()}

    execute('install_gatehouse', 'TEST_ONLY_INSTALL_PASSWORD\nTEST_ONLY_INSTALL_PASSWORD\n')
    assert (prefix / 'config/state.json').is_file() and not (prefix / 'data/state.json').exists()
    for folder in ('log', 'data'): assert (prefix / folder).is_dir()
    (prefix / 'log/calls.jsonl').write_text('{"id":1,"message":"TEST_ONLY_LOG"}\n')
    (prefix / 'log/calls.jsonl.1').write_text('{"id":0,"message":"TEST_ONLY_ROTATED_LOG"}\n')
    for folder in ('certificates', 'route-images', 'subscriptions'):
        (prefix / 'data' / folder).mkdir(mode=0o700)
        (prefix / 'data' / folder / 'fixture').write_bytes(b'TEST_ONLY_PERSISTENT_DATA')
    before = snapshot()
    execute('install_gatehouse')
    assert snapshot() == before, 'Reinstall reset saved credentials or files.'
    execute('uninstall_gatehouse', 'UNINSTALL\n\n')
    assert snapshot() == before and not (prefix / 'launcher').exists() and not (prefix / 'app').exists()
    print('Installer: first install, password initialization, reinstall and uninstall retain config/log/data: PASS')

    shutil.copy2(prefix / 'config/state.json', legacy / 'state.json')
    shutil.copytree(prefix / 'log', legacy / 'logs')
    for folder in ('certificates', 'route-images', 'subscriptions'):
        shutil.copytree(prefix / 'data' / folder, legacy / folder)
    environment['QA_PREFIX'] = str(temp / 'migrated')
    old_files = {p.relative_to(legacy).as_posix(): p.read_bytes() for p in legacy.rglob('*') if p.is_file()}
    execute('install_gatehouse')
    migrated = temp / 'migrated'
    assert (migrated / 'config/state.json').read_bytes() == old_files['state.json']
    for name, data in old_files.items():
        if name == 'state.json': target = migrated / 'config/state.json'
        elif name.startswith('logs/'): target = migrated / 'log' / name[5:]
        else: target = migrated / 'data' / name
        assert target.read_bytes() == data
        assert (legacy / name).read_bytes() == data
    print('Installer: old Linux layout migrates all files and retains the original data: PASS')

    execute('SOURCE_DIR=""; load_package amd64; test -f "$SOURCE_DIR/dist/gatehouse-linux-amd64"')
    execute('SOURCE_DIR=""; load_package arm64; test -f "$SOURCE_DIR/dist/gatehouse-linux-arm64"')
    environment['QA_DOWNLOAD_FAILURES'] = '0'
    for option, proxy in (('--proxy', 'http://proxy.example.test:7890'), ('-x', 'socks5h://proxy.example.test:1080')):
        environment['QA_EXPECT_PROXY'] = proxy
        environment['NO_PROXY'] = '*'
        output = execute('install_gatehouse() { SOURCE_DIR=""; load_package amd64; }; main install ' + option + ' "$QA_EXPECT_PROXY"')
        assert proxy.encode() not in output, 'Proxy address was printed.'
    environment.pop('QA_EXPECT_PROXY')
    environment.pop('NO_PROXY')
    execute('main install --proxy', success=False)
    execute('main install uninstall', success=False)
    print('Installer: HTTP/SOCKS proxy flags reach archive and checksum downloads, override NO_PROXY and remain private: PASS')
    counter = temp / 'curl-attempts'
    counter.unlink()
    environment['QA_DOWNLOAD_FAILURES'] = '1'
    execute('SOURCE_DIR=""; load_package amd64; cmp "$SOURCE_DIR/dist/gatehouse-linux-amd64" "$QA_PACKAGE/dist/gatehouse-linux-amd64"')
    assert counter.read_text() == '2', 'SSL read failure was not retried.'
    counter.unlink()
    environment['QA_DOWNLOAD_FAILURES'] = '4'
    environment['QA_PREFIX'] = str(temp / 'network-failed-install')
    execute('SOURCE_DIR=""; install_gatehouse', success=False)
    assert counter.read_text() == '4' and not (temp / 'network-failed-install').exists()
    assert not list((temp / 'tmp').iterdir()), 'Partial download files were retained.'
    print('Installer: curl 56 partial download retries cleanly; repeated failure leaves installation untouched: PASS')
    environment['QA_DOWNLOAD_FAILURES'] = '0'
    archive = assets / 'gatehome-linux-amd64.tar.gz'
    archive.write_bytes(archive.read_bytes() + b'TEST_ONLY_CORRUPTION')
    execute('SOURCE_DIR=""; load_package amd64', success=False)
    print('Installer: curl download selects both architectures and rejects checksum corruption: PASS')
