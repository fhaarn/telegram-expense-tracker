#!/usr/bin/env python3
"""Deploy one tested commit. Intended for an explicitly enabled CI job, not startup."""
import json
import os
import re
import sys
import time
import urllib.error
import urllib.parse
import urllib.request

class DeployError(Exception):
    pass

class NoRedirects(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        raise DeployError("Unexpected redirect; request stopped")

class RenderAPI:
    def __init__(self, token):
        self.token = token
        self.opener = urllib.request.build_opener(NoRedirects())

    def request(self, method, path, payload=None):
        data = None if payload is None else json.dumps(payload).encode()
        request = urllib.request.Request(
            "https://api.render.com/v1" + path, data=data, method=method,
            headers={"Authorization": "Bearer " + self.token,
                     "Content-Type": "application/json", "Accept": "application/json"})
        try:
            with self.opener.open(request, timeout=30) as response:
                return json.load(response)
        except urllib.error.HTTPError as exc:
            raise DeployError(f"Render API returned HTTP {exc.code}") from None
        except (OSError, ValueError):
            # Provider bodies/exception URLs may include confidential information.
            raise DeployError("Render API request failed; inspect the Render dashboard") from None


def check_health(url):
    try:
        opener = urllib.request.build_opener(NoRedirects())
        with opener.open(url, timeout=15) as response:
            return response.status == 200 and json.load(response).get("status") == "ok"
    except (OSError, ValueError, DeployError):
        return False


def deploy(api, service_id, commit, health_url, *, sleep=time.sleep,
           clock=time.monotonic, health=check_health, timeout=900):
    base = "/services/" + service_id + "/deploys"
    # Do not automatically repeat this POST: an ambiguous timeout may have created a deploy.
    created = api.request("POST", base, {"commitId": commit, "clearCache": "do_not_clear"})
    deploy_id = created.get("id", "")
    if not re.fullmatch(r"dep-[a-zA-Z0-9]+", deploy_id):
        raise DeployError("Render did not return a valid deploy ID; inspect dashboard before retrying")
    print("Deploy requested:", deploy_id, flush=True)
    deadline = clock() + timeout
    terminal_failures = {"build_failed", "pre_deploy_failed", "update_failed", "canceled", "deactivated"}
    while clock() < deadline:
        state = api.request("GET", base + "/" + deploy_id)
        status = state.get("status")
        if status in terminal_failures:
            raise DeployError("Render deployment ended with status: " + status)
        if status == "live":
            if state.get("commit", {}).get("id") != commit:
                raise DeployError("Live deployment does not match the tested commit")
            for _ in range(12):
                if health(health_url):
                    print("Tested commit is live and the health endpoint passed", flush=True)
                    return deploy_id
                sleep(5)
            raise DeployError("Deployment became live, but health verification failed")
        sleep(10)
    raise DeployError("Deployment timed out; inspect Render before retrying")


def main():
    token = os.environ.get("RENDER_API_KEY", "")
    service = os.environ.get("RENDER_SERVICE_ID", "")
    commit = os.environ.get("GITHUB_SHA", "")
    url = os.environ.get("RENDER_HEALTH_URL", "")
    parsed = urllib.parse.urlsplit(url)
    if not token or not re.fullmatch(r"srv-[a-zA-Z0-9]+", service):
        raise DeployError("RENDER_API_KEY and a valid RENDER_SERVICE_ID are required")
    if not re.fullmatch(r"[a-fA-F0-9]{40}|[a-fA-F0-9]{64}", commit):
        raise DeployError("GITHUB_SHA must identify the tested commit")
    if (parsed.scheme != "https" or not parsed.hostname or parsed.username
            or parsed.password or parsed.query or parsed.fragment or parsed.path != "/healthz"):
        raise DeployError("RENDER_HEALTH_URL must be a public HTTPS /healthz URL without credentials")
    deploy(RenderAPI(token), service, commit, url)


if __name__ == "__main__":
    try:
        main()
    except DeployError as exc:
        print("Deployment failed:", exc, file=sys.stderr)
        sys.exit(1)
