"""Manual PostgreSQL backup/restore without putting credentials in process arguments."""
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import urllib.parse


class DatabaseError(Exception):
    pass


def connection_environment(url):
    try:
        u = urllib.parse.urlsplit(url)
        if u.scheme not in ('postgres', 'postgresql') or not u.hostname or not u.username or not u.path.strip('/'):
            raise ValueError()
        env = {k: v for k, v in os.environ.items() if not k.startswith('PG')}
        env.update(PGHOST=u.hostname, PGPORT=str(u.port or 5432), PGUSER=urllib.parse.unquote(u.username),
                   PGDATABASE=urllib.parse.unquote(u.path[1:]), PGCONNECT_TIMEOUT='10')
        if u.password is not None:
            env['PGPASSWORD'] = urllib.parse.unquote(u.password)
        supported = {'sslmode': 'PGSSLMODE', 'sslrootcert': 'PGSSLROOTCERT', 'sslcert': 'PGSSLCERT',
                     'sslkey': 'PGSSLKEY', 'channel_binding': 'PGCHANNELBINDING', 'application_name': 'PGAPPNAME'}
        for key, value in urllib.parse.parse_qsl(u.query, strict_parsing=True):
            if key not in supported:
                raise ValueError()
            env[supported[key]] = value
        if u.fragment:
            raise ValueError()
        if u.hostname not in ('localhost', '127.0.0.1', '::1') and env.get('PGSSLMODE') != 'verify-full':
            raise DatabaseError('Remote database URLs must use sslmode=verify-full')
        return env
    except (ValueError, TypeError):
        raise DatabaseError('Use a PostgreSQL URL with host, user, database and supported TLS options') from None


def run(args, env):
    try:
        result = subprocess.run(args, env=env, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=False)
    except OSError:
        raise DatabaseError('PostgreSQL client tool could not run') from None
    if result.returncode:
        raise DatabaseError('Database operation failed; check connectivity, permissions and client version')
    return result.stdout.strip()


def backup(destination, env):
    path = Path(destination)
    if os.path.lexists(path):
        raise DatabaseError('Refusing to overwrite an existing backup')
    fd, temp = tempfile.mkstemp(prefix=path.name + '.tmp-', dir=path.parent)
    os.close(fd)
    try:
        run(['pg_dump', '--format=custom', '--no-owner', '--no-acl', '--file=' + temp], env)
        run(['pg_restore', '--list', temp], env)
        os.link(temp, path)
    finally:
        os.unlink(temp)
    print('Backup created and archive validated; store it securely off-host')


def restore(archive, env):
    if os.environ.get('CONFIRM_RESTORE') != 'empty-database':
        raise DatabaseError('Set CONFIRM_RESTORE=empty-database after checking the target')
    count = run(['psql', '-X', '-A', '-t', '-v', 'ON_ERROR_STOP=1', '-c',
        "SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname NOT IN ('pg_catalog','information_schema') AND n.nspname NOT LIKE 'pg_toast%' AND c.relkind IN ('r','p','v','m','S','f')"], env)
    if count != '0':
        raise DatabaseError('Restore target is not empty; refusing to modify it')
    run(['pg_restore', '--dbname=' + env['PGDATABASE'], '--single-transaction', '--exit-on-error', '--no-owner', '--no-acl', archive], env)
    print('Restore completed; verify records and reports before changing application traffic')


def main():
    if len(sys.argv) != 3 or sys.argv[1] not in ('backup', 'restore'):
        raise DatabaseError('Usage: database.py backup|restore archive.dump')
    for tool in ('pg_dump', 'pg_restore', 'psql'):
        if not shutil.which(tool):
            raise DatabaseError(tool + ' is required (PostgreSQL client tools)')
    mode = sys.argv[1]
    key = 'DATABASE_URL' if mode == 'backup' else 'RESTORE_DATABASE_URL'
    env = connection_environment(os.environ.get(key, ''))
    if mode == 'backup':
        backup(sys.argv[2], env)
    else:
        restore(sys.argv[2], env)


if __name__ == '__main__':
    try:
        main()
    except (DatabaseError, OSError) as exc:
        print(str(exc) if isinstance(exc, DatabaseError) else 'Backup/restore file operation failed', file=sys.stderr)
        sys.exit(1)
