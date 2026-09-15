import unittest
from unittest.mock import patch
import deploy_render

SHA = "a" * 40

class FakeAPI:
    def __init__(self, states):
        self.states = iter(states)
        self.calls = []
    def request(self, method, path, payload=None):
        self.calls.append((method, path, payload))
        if method == "POST":
            return {"id": "dep-test123"}
        return next(self.states)

class DeploymentTests(unittest.TestCase):
    def test_exact_commit_and_health(self):
        api = FakeAPI([{"status": "build_in_progress"}, {"status": "live", "commit": {"id": SHA}}])
        result = deploy_render.deploy(api, "srv-test123", SHA, "https://example.com/healthz",
                                      sleep=lambda _: None, health=lambda _: True)
        self.assertEqual(result, "dep-test123")
        self.assertEqual(api.calls[0][2]["commitId"], SHA)
        self.assertEqual(sum(c[0] == "POST" for c in api.calls), 1)
    def test_reject_wrong_commit(self):
        api = FakeAPI([{"status": "live", "commit": {"id": "b" * 40}}])
        with self.assertRaisesRegex(deploy_render.DeployError, "tested commit"):
            deploy_render.deploy(api, "srv-test123", SHA, "https://example.com/healthz", health=lambda _: True)
    def test_failed_deploy(self):
        with self.assertRaisesRegex(deploy_render.DeployError, "build_failed"):
            deploy_render.deploy(FakeAPI([{"status":"build_failed"}]), "srv-test123", SHA, "https://example.com/healthz")
    def test_health_failure(self):
        with self.assertRaisesRegex(deploy_render.DeployError, "health verification"):
            deploy_render.deploy(FakeAPI([{"status":"live", "commit":{"id":SHA}}]), "srv-test123", SHA,
                                 "https://example.com/healthz", sleep=lambda _:None, health=lambda _:False)
    def test_timeout(self):
        ticks = iter([0, 901])
        with self.assertRaisesRegex(deploy_render.DeployError, "timed out"):
            deploy_render.deploy(FakeAPI([]), "srv-test123", SHA, "https://example.com/healthz", clock=lambda:next(ticks))
    def test_invalid_config_never_calls_provider(self):
        with patch.dict("os.environ", {"RENDER_API_KEY":"private-token", "RENDER_SERVICE_ID":"srv-test123",
                                     "GITHUB_SHA":SHA, "RENDER_HEALTH_URL":"https://user:password@example.com/healthz"}, clear=True):
            with self.assertRaises(deploy_render.DeployError), patch.object(deploy_render.RenderAPI, "request") as request:
                deploy_render.main()
            request.assert_not_called()

if __name__ == "__main__":
    unittest.main()
