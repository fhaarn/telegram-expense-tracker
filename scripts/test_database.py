import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
import database


class DatabaseToolsTests(unittest.TestCase):
    def test_url_to_environment(self):
        env = database.connection_environment('postgres://user:p%40ss@localhost:5433/example?sslmode=disable')
        self.assertEqual(env['PGPASSWORD'], 'p@ss')
        self.assertEqual(env['PGHOST'], 'localhost')
        self.assertEqual(env['PGPORT'], '5433')

    def test_remote_requires_verified_tls(self):
        with self.assertRaises(database.DatabaseError):
            database.connection_environment('postgres://user:secret@db.example.com/app')
        env = database.connection_environment('postgres://user:secret@db.example.com/app?sslmode=verify-full')
        self.assertEqual(env['PGSSLMODE'], 'verify-full')

    def test_existing_backup_never_overwritten(self):
        with tempfile.TemporaryDirectory() as folder:
            path = Path(folder) / 'backup.dump'
            path.write_text('original')
            with self.assertRaises(database.DatabaseError):
                database.backup(path, {})
            self.assertEqual(path.read_text(), 'original')

    def test_restore_requires_empty_target(self):
        with patch.dict(os.environ, {'CONFIRM_RESTORE': 'empty-database'}), patch.object(database, 'run', return_value='1') as run:
            with self.assertRaises(database.DatabaseError):
                database.restore('file.dump', {'PGDATABASE': 'test'})
            self.assertEqual(run.call_count, 1)

    def test_no_url_in_process_arguments(self):
        env = database.connection_environment('postgres://user:secret@localhost/test')
        with patch.dict(os.environ, {'CONFIRM_RESTORE': 'empty-database'}), patch.object(database, 'run', side_effect=['0', '']) as run:
            database.restore('file.dump', env)
            self.assertNotIn('secret', str(run.call_args.args[0]))
