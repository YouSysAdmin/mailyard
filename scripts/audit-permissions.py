#!/usr/bin/env python3
"""Ask a RUNNING server whether every route is reachable by exactly the
permission it claims.

The static guards pin the declarations to each other: routes.go against
the OpenAPI document, the console's nav strings against the catalogue,
every group against a resource. None of them presses the button. This
does - it mints credentials, sends requests and reads the answers, and
it is the only check here that would notice a middleware ordered wrong
or a handler that forgot to ask.

Three surfaces, because they are gated three different ways:

  1. /api/v1 routes carrying permOn - driven from the exported OpenAPI
     document, which TestDocumentedPermissionsMatchTheRouter has
     already pinned to the router.
  2. /api/v1/projects/* - decided inside handlers, because those routes
     address a project by PATH id. Probed with real signed-in members,
     since a key has no membership to read.
  3. /api/v1/admin/* - not a permission at all but a different
     credential, so the check is that nothing else gets in.

Whether a request then succeeds is irrelevant. A 400 or a 404 means the
gate let it through, which is the only thing being measured.

  task audit-perms          stands up a throwaway instance and runs this
  AUDIT_URL=... AUDIT_ADMIN_PW=... AUDIT_SPEC=openapi.yaml python3 scripts/audit-permissions.py
"""
import json
import os
import re
import sys
import urllib.error
import urllib.request
import http.cookiejar

BASE = os.environ["AUDIT_URL"].rstrip("/")
ADMIN_EMAIL = os.environ.get("AUDIT_ADMIN_EMAIL", "admin@example.test")
ADMIN_PW = os.environ["AUDIT_ADMIN_PW"]
SPEC = os.environ["AUDIT_SPEC"]
FAKE = "00000000-0000-4000-8000-000000000000"

# A refusal BY AUTHORIZATION, as opposed to a 404, a validation 400 or
# any other 403 a handler may raise for its own reasons.
REFUSAL = re.compile(
    r"^(permission [a-z]+:[a-z]+ required"
    r"|no access to [a-z]+ in this project"
    r"|not a project member"
    r"|insufficient permissions"
    r"|only a project owner"
    r"|the members:delete)"
)


def session():
    jar = http.cookiejar.CookieJar()
    op = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))

    def call(method, path, body=None, headers=None, token=None):
        data = json.dumps(body).encode() if body is not None else None
        req = urllib.request.Request(BASE + path, data=data, method=method)
        req.add_header("Content-Type", "application/json")
        for k, v in (headers or {}).items():
            req.add_header(k, v)
        if token:
            req.add_header("Authorization", "Bearer " + token)
        try:
            with op.open(req) as r:
                raw = r.read()
                try:
                    return r.status, json.loads(raw) if raw else {}
                except ValueError:
                    return r.status, {}
        except urllib.error.HTTPError as e:
            raw = e.read()
            try:
                return e.code, json.loads(raw) if raw else {}
            except ValueError:
                return e.code, {}

    return call


def refused(status, body):
    return status == 403 and bool(REFUSAL.match(body.get("error", "")))


def routes_from_spec(path):
    """Every documented route and the permission its description names.

    Parsed rather than imported: the document is what an integrator
    reads, so driving from it also checks that what they read is true.
    """
    text = open(path, encoding="utf8").read()
    out, route, method = {}, None, None
    for line in text.split("\n"):
        m = re.match(r"^    (/\S*):\s*$", line)
        if m:
            route, method = m.group(1), None
            continue
        m = re.match(r"^        (get|post|put|patch|delete):\s*$", line)
        if m:
            method = m.group(1).upper()
            continue
        m = re.search(r"(?:Needs|Requires) the `([a-z]+:[a-z]+)` permission", line)
        if m and route and method:
            out[method + " " + route] = m.group(1)
    return out


fails = []
probes = 0
# A 5xx anywhere. The permission checks read a refusal or the absence
# of one, so a handler that panics or sends broken SQL counts as
# "the gate let it through" and passes silently - which is exactly how
# a campaign analytics query naming a column that never existed
# answered 500 from the day it was written without failing anything.
server_errors = {}


def note(method, url, status, body):
    if status >= 500:
        server_errors[method + " " + url.split("?")[0]] = str(status) + " " + str(body.get("error", ""))[:80]


def check(cond, message):
    global probes
    probes += 1
    if not cond:
        fails.append(message)


admin = session()
status, _ = admin("POST", "/app/api/auth/login", {"email": ADMIN_EMAIL, "password": ADMIN_PW})
if status != 200:
    sys.exit(f"could not sign in as {ADMIN_EMAIL}: {status}")
project = admin("POST", "/api/v1/projects", {"name": "Permission audit"})[1]["project"]["id"]
H = {"X-Mailyard-Project-Id": project}
catalogue = {
    d["resource"]: d["actions"]
    for d in admin("GET", "/api/v1/permissions", None, H)[1]["resources"]
}

# --- 1. the permOn surface, one API key per permission ----------------
declared = routes_from_spec(SPEC)
if len(declared) < 100:
    sys.exit(f"only found {len(declared)} documented permissions in {SPEC} - the parse is broken")

keys = {"": admin("POST", "/api/v1/api-keys", {"name": "audit-none", "permissions": []}, H)[1]["token"]}
for perm in sorted(set(declared.values())):
    keys[perm] = admin(
        "POST", "/api/v1/api-keys", {"name": "audit-" + perm, "permissions": [perm]}, H
    )[1]["token"]

for route, need in sorted(declared.items()):
    method, path = route.split(" ", 1)
    url = "/api/v1" + re.sub(r"\{[A-Za-z_]+\}", FAKE, path)
    body = {} if method in ("POST", "PUT", "PATCH", "DELETE") else None
    resource, action = need.split(":")

    status, b = admin(method, url, body, H, keys[need])
    note(method, url, status, b)
    check(not refused(status, b), f"{route}: holding {need} was REFUSED ({b.get('error')})")

    for other in catalogue.get(resource, []):
        sibling = f"{resource}:{other}"
        if other == action or sibling not in keys:
            continue
        status, b = admin(method, url, body, H, keys[sibling])
        check(refused(status, b), f"{route}: needs {need} but {sibling} got through ({status})")

    status, b = admin(method, url, body, H, keys[""])
    check(refused(status, b), f"{route}: needs {need} but a key with NO permissions got through ({status})")

print(f"{len(declared)} documented routes probed by permission")

# --- 2. the handler-enforced project surface --------------------------
# What each route is SUPPOSED to demand. Hand-written because it is
# hand-enforced - that is the whole risk being covered.
HANDLER = [
    ("GET", "/projects/{p}", "member"),
    ("PATCH", "/projects/{p}", "settings:write"),
    ("DELETE", "/projects/{p}", "owner"),
    ("GET", "/projects/{p}/members", "members:read"),
    ("POST", "/projects/{p}/members", "members:write"),
    ("PATCH", "/projects/{p}/members/" + FAKE, "members:write"),
    ("DELETE", "/projects/{p}/members/" + FAKE, "members:delete"),
    ("GET", "/projects/{p}/roles", "members:read"),
    ("POST", "/projects/{p}/roles", "members:write"),
    ("PATCH", "/projects/{p}/roles/" + FAKE, "members:write"),
    ("DELETE", "/projects/{p}/roles/" + FAKE, "members:delete"),
    ("PUT", "/projects/{p}/default-role", "members:write"),
    ("GET", "/projects/{p}/invitations", "members:write"),
    ("POST", "/projects/{p}/invitations", "members:write"),
    ("DELETE", "/projects/{p}/invitations/" + FAKE, "members:delete"),
]
GOVERNANCE = ["members:read", "members:write", "members:delete", "settings:read", "settings:write"]

people = {}
for perm in GOVERNANCE + [""]:
    email = "audit-" + (perm or "nothing").replace(":", "-") + "@example.invalid"
    admin("POST", "/api/v1/admin/users", {"email": email, "password": "Passw0rd!x", "role": "user"})
    admin("POST", f"/api/v1/projects/{project}/members", {"email": email})
    if perm:
        role = admin(
            "POST", f"/api/v1/projects/{project}/roles",
            {"name": "audit-" + perm.replace(":", "-"), "permissions": [perm]}, H,
        )[1]["role"]["id"]
        uid = [
            m["user_id"]
            for m in admin("GET", f"/api/v1/projects/{project}/members", None, H)[1]["members"]
            if m.get("email") == email
        ][0]
        admin("PATCH", f"/api/v1/projects/{project}/members/{uid}", {"role_id": role}, H)
    s = session()
    if s("POST", "/app/api/auth/login", {"email": email, "password": "Passw0rd!x"})[0] != 200:
        sys.exit("could not sign in the audit member " + email)
    people[perm] = s

for method, template, need in HANDLER:
    url = "/api/v1" + template.format(p=project)
    body = {} if method in ("POST", "PUT", "PATCH", "DELETE") else None
    for perm, who in people.items():
        status, b = who(method, url, body, H)
        note(method, url, status, b)
        # None of these members owns the project, so an owner-only
        # route must refuse every one of them.
        should_pass = need == "member" or (need == perm and need != "owner")
        held = perm or "nothing"
        if should_pass:
            check(not refused(status, b),
                  f"{method} {template} needs {need}: holding {held} was REFUSED ({status})")
        else:
            check(refused(status, b),
                  f"{method} {template} needs {need}: holding {held} got through ({status})")

print(f"{len(HANDLER)} handler-enforced routes probed by membership")

# --- 3. platform administration is a CREDENTIAL, not a permission -----
wildcard = admin("POST", "/api/v1/api-keys", {"name": "audit-wildcard", "permissions": ["*"]}, H)[1]["token"]
ADMIN_ROUTES = [
    ("GET", "/api/v1/admin/users"), ("POST", "/api/v1/admin/users"),
    ("GET", "/api/v1/admin/settings"), ("PUT", "/api/v1/admin/settings"),
    ("GET", "/api/v1/admin/plans"), ("GET", "/api/v1/admin/api-keys"),
    ("GET", "/api/v1/admin/shared-smtp-servers"), ("GET", "/api/v1/admin/relay-nodes"),
    ("GET", "/api/v1/admin/oauth-providers"),
    # /api/v1/projects/empty-personal was here and is gone with personal
    # projects themselves. It stopped naming a route and started matching
    # GET /projects/:id with a garbage id, so the audit was measuring an
    # id that cannot be a uuid rather than an admin route - which is how
    # the 500 that used to answer that was found.
]
for method, path in ADMIN_ROUTES:
    body = {} if method in ("POST", "PUT") else None
    status, _ = admin(method, path, body, H, wildcard)
    check(status == 403, f"{method} {path}: a project key holding * got {status}, want 403")
    status, _ = people["members:write"](method, path, body, H)
    check(status == 403, f"{method} {path}: an ordinary member got {status}, want 403")

print(f"{len(ADMIN_ROUTES)} admin routes probed against a project key and a member")

if server_errors:
    print(f"\n{len(server_errors)} route(s) answered 5xx:")
    for k in sorted(server_errors):
        print(f"  {k}: {server_errors[k]}")
    fails.append(f"{len(server_errors)} route(s) answered 5xx - see the list above")

print(f"\n{probes} probes")
if fails:
    print(f"\n{len(fails)} PROBLEM(S):")
    for f in sorted(set(fails)):
        print("  " + f)
    sys.exit(1)
print("every route is reachable by exactly the permission it claims")
