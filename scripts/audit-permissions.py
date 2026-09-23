#!/usr/bin/env python3
"""Ask a RUNNING server whether every route is reachable by exactly the
permission it claims.

The static guards pin the declarations to each other: routes.go against
the OpenAPI document, the console's nav strings against the catalogue,
every group against a resource. None of them presses the button. This
does - it mints credentials, sends requests and reads the answers, and
it is the only check here that would notice a middleware ordered wrong
or a handler that forgot to ask.

What is probed:

  1. /api/v1 routes carrying permOn - driven from the exported OpenAPI
     document, which TestDocumentedPermissionsMatchTheRouter has
     already pinned to the router.
  2. /api/v1/projects/* - decided inside handlers, because those routes
     address a project by PATH id. Probed with real signed-in members,
     since a key has no membership to read.
  3. /api/v1/admin/* - not a permission at all but a different
     credential, so the check is that nothing else gets in.
  4. Tenancy - one of everything in project B, then every id-addressed
     route and every list asked by project A's key and owner. B's id
     must get the same answer as an id that exists nowhere, no answer
     may name B's data, and B must hold the same things afterwards.
  5. Delegation - a member may not hand out a role wider than their own.
  6. /app/api - nothing but a session gets past its gate.
  7. A sandbox credential reaches no real mail.

Whether a request then succeeds is irrelevant. A 400 or a 404 means the
gate let it through, which is the only thing being measured.

  task audit-perms          stands up a throwaway instance and runs this
  AUDIT_URL=... AUDIT_ADMIN_PW=... AUDIT_SPEC=openapi.yaml AUDIT_APP_SPEC=app.yaml \
    AUDIT_PG_CONTAINER=... python3 scripts/audit-permissions.py
"""
import json
import os
import re
import subprocess
import sys
import urllib.error
import urllib.request
import http.cookiejar

BASE = os.environ["AUDIT_URL"].rstrip("/")
ADMIN_EMAIL = os.environ.get("AUDIT_ADMIN_EMAIL", "admin@example.test")
ADMIN_PW = os.environ["AUDIT_ADMIN_PW"]
SPEC = os.environ["AUDIT_SPEC"]
APP_SPEC = os.environ.get("AUDIT_APP_SPEC", "")
PG_CONTAINER = os.environ["AUDIT_PG_CONTAINER"]
FAKE = "00000000-0000-4000-8000-000000000000"
PW = "Passw0rd!x-audit"
UUID = re.compile(r"[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}")

# A refusal BY AUTHORIZATION, as opposed to a 404, a validation 400 or
# any other 403 a handler may raise for its own reasons.
REFUSAL = re.compile(
    r"^(permission [a-z]+:[a-z]+ required"
    r"|no access to [a-z]+ in this project"
    r"|not a project member"
    r"|insufficient permissions"
    r"|only a project owner"
    r"|the members:delete"
    r"|this is a sandbox credential)"
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
            with op.open(req, timeout=20) as r:
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


def all_routes(path):
    """Every (method, path) the document lists, gated or not."""
    out, route = [], None
    for line in open(path, encoding="utf8").read().split("\n"):
        m = re.match(r"^    (/\S*):\s*$", line)
        if m:
            route = m.group(1)
            continue

        m = re.match(r"^        (get|post|put|patch|delete):\s*$", line)
        if m and route:
            out.append((m.group(1).upper(), route))

    return out


def must(result, what):
    """A fixture the audit cannot build is a hole in what it measures,
    so it stops the run rather than shrinking the coverage quietly."""
    status, body = result
    if status >= 300:
        sys.exit(f"setup failed: {what}: {status} {body.get('error', body)}")

    return body


def sql(statement):
    """For the one fixture the API cannot build: a domain proves
    ownership through DNS, which a throwaway instance does not have."""
    subprocess.run(
        ["docker", "exec", PG_CONTAINER, "psql", "-U", "postgres", "-d", "audit", "-q", "-c", statement],
        check=True, stdout=subprocess.DEVNULL,
    )


def first_id(body):
    """The id of whatever a create call answered with."""
    if isinstance(body, dict):
        if isinstance(body.get("id"), str):
            return body["id"]

        for v in body.values():
            if isinstance(v, dict) and isinstance(v.get("id"), str):
                return v["id"]

    return None


fails = []
probes = 0
# A 5xx anywhere. The permission checks read a refusal or the absence
# of one, so a handler that panics or sends broken SQL would count as
# "the gate let it through" and pass silently.
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
    must(admin("POST", "/api/v1/admin/users", {"email": email, "password": PW}), "create " + email)
    must(admin("POST", f"/api/v1/projects/{project}/members", {"email": email}), "add " + email)
    if perm:
        role = must(admin(
            "POST", f"/api/v1/projects/{project}/roles",
            {"name": "audit-" + perm.replace(":", "-"), "permissions": [perm]}, H,
        ), "role " + perm)["role"]["id"]
        uid = [
            m["user_id"]
            for m in admin("GET", f"/api/v1/projects/{project}/members", None, H)[1]["members"]
            if m.get("email") == email
        ][0]
        must(admin("PATCH", f"/api/v1/projects/{project}/members/{uid}", {"role_id": role}, H), "assign " + perm)
    s = session()
    if s("POST", "/app/api/auth/login", {"email": email, "password": PW})[0] != 200:
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

# --- tenants for the sections below -----------------------------------
# A is owned by an ordinary account, so nothing it reaches is reached
# through the platform-admin exemption. B is the victim.
ROUTES = all_routes(SPEC)
UA_EMAIL = "audit-tenant-a@example.invalid"
must(admin("POST", "/api/v1/admin/users", {"email": UA_EMAIL, "password": PW}), "create tenant A owner")
ua = session()
must(ua("POST", "/app/api/auth/login", {"email": UA_EMAIL, "password": PW}), "sign in tenant A owner")
A = must(admin("POST", "/api/v1/projects", {"name": "Tenant A"}), "project A")["project"]["id"]
HA = {"X-Mailyard-Project-Id": A}
must(admin("POST", f"/api/v1/projects/{A}/members", {"email": UA_EMAIL}), "add tenant A owner")
ua_id = [m["user_id"] for m in must(admin("GET", f"/api/v1/projects/{A}/members", None, HA), "A members")["members"]
         if m.get("email") == UA_EMAIL][0]
must(admin("PATCH", f"/api/v1/projects/{A}/members/{ua_id}", {"owner": True}, HA), "make tenant A owner")
kA = must(ua("POST", "/api/v1/api-keys", {"name": "a-wildcard", "permissions": ["*"]}, HA), "A key")["token"]
anon = session()

B = must(admin("POST", "/api/v1/projects", {"name": "Tenant B"}), "project B")["project"]["id"]
HB = {"X-Mailyard-Project-Id": B}

# --- 3. platform administration is a CREDENTIAL, not a permission -----
admin_routes = [(m, p) for m, p in ROUTES if p.startswith("/admin")]
for method, path in admin_routes:
    url = "/api/v1" + re.sub(r"\{[A-Za-z_]+\}", FAKE, path)
    body = {} if method in ("POST", "PUT", "PATCH", "DELETE") else None
    for who, call in (
        ("a project key holding *", lambda: anon(method, url, body, HA, kA)),
        ("a project owner's session", lambda: ua(method, url, body, HA)),
        ("an ordinary member", lambda: people["members:write"](method, url, body, H)),
        ("no credential", lambda: anon(method, url, body)),
    ):
        status, b = call()
        note(method, url, status, b)
        check(status in (401, 403), f"{method} {path}: {who} got {status}, want 401/403")

print(f"{len(admin_routes)} admin routes probed against a project key, an owner, a member and nobody")

# --- 4. another project's resource looks like a missing one -----------
# One of everything in B, then every id-addressed route asked by A with
# B's ids and again with an id that exists nowhere. The two answers must
# be the same answer: any difference is either a leak or an oracle.
FIXTURES = [
    ("api-keys", "/api-keys", {"name": "b-key", "permissions": ["templates:read"]}),
    ("smtp-server-groups", "/smtp-server-groups", {"name": "b-group"}),
    ("smtp-servers", "/smtp-servers",
     {"name": "b-smtp", "host": "smtp.example.com", "port": 587, "encryption": "starttls"}),
    ("smtp-credentials", "/smtp-credentials", {"name": "b-cred"}),
    ("domains", "/domains", {"domain": "tenant-b.example"}),
    ("senders", "/senders", {"email": "hello@tenant-b.example", "name": "B"}),
    ("languages", "/languages", {"code": "fr", "name": "French"}),
    ("stylesheets", "/stylesheets", {"name": "b-css", "css": "p{color:red}"}),
    ("templates", "/templates", {"name": "b-tpl", "subject": "Hi", "html": "<p>hi</p>"}),
    ("subscribers", "/subscribers", {"email": "sub@tenant-b.example"}),
    ("subscriber-lists", "/subscriber-lists", {"name": "b-list"}),
    ("unsubscribe-lists", "/unsubscribe-lists", {"name": "b-unsub"}),
    ("webhooks", "/webhooks", {"url": "https://example.com/hook", "events": ["email.sent"]}),
    ("sandbox/inboxes", "/sandbox/inboxes", {"name": "b-inbox", "addresses": ["dev@tenant-b.example"]}),
    ("sandbox/credentials", "/sandbox/credentials", {"name": "b-sbx"}),
    ("suppressions", "/suppressions", {"email": "blocked@tenant-b.example"}),
]
ids, bodies = {}, {}
for family, path, body in FIXTURES:
    ids[family] = first_id(must(admin("POST", "/api/v1" + path, body, HB), "B " + family))
    bodies[family] = body
    if family == "domains":
        sql(f"UPDATE domains SET verified = true, verified_at = now() WHERE id = '{ids[family]}'")

camp = {"name": "b-camp", "from_email": "hello@tenant-b.example", "subject": "Hi",
        "template_id": ids["templates"], "list_id": ids["subscriber-lists"]}
ids["campaigns"] = first_id(must(admin("POST", "/api/v1/campaigns", camp, HB), "B campaign"))
bodies["campaigns"] = camp

versions = must(admin("GET", f"/api/v1/templates/{ids['templates']}/versions", None, HB), "B versions")
ids["versionId"] = first_id(next(iter(v for v in versions.values() if isinstance(v, list)))[0])
must(admin("POST", f"/api/v1/subscriber-lists/{ids['subscriber-lists']}/members",
           {"subscriber_id": ids["subscribers"]}, HB), "B list member")
ids["subscriberId"] = ids["subscribers"]

# A captured message, through a sandbox key of B's own.
sbx = must(admin("POST", "/api/v1/api-keys",
                 {"name": "b-sandbox", "sandbox": True, "permissions": ["sandbox:read", "sandbox:write"]}, HB),
           "B sandbox key")["token"]
must(anon("POST", "/api/v1/emails/send", {"from": "hello@tenant-b.example", "to": ["dev@tenant-b.example"],
                                          "subject": "s", "html": "<p>x</p>"}, HB, sbx), "B sandbox send")
listing = must(admin("GET", "/api/v1/sandbox", None, HB), "B sandbox list")
ids["sandbox"] = first_id(next(iter(v for v in listing.values() if isinstance(v, list)))[0])

audit = must(admin("GET", "/api/v1/audit-log", None, HB), "B audit log")
rows = next(iter(v for v in audit.values() if isinstance(v, list)), [])
if rows:
    ids["audit-log"] = rows[0]["id"]

b_ids = {v for v in ids.values() if v} | {B}


def family_of(path):
    head = path.split("/{", 1)[0].strip("/")
    return head if head in ids else None


def substitute(path, real):
    def one(m):
        name = m.group(1)
        if name == "idx":
            return "0"

        if not real:
            return FAKE

        if name == "id":
            return ids[family_of(path)]

        return ids.get(name, FAKE)

    return re.sub(r"\{([A-Za-z_]+)\}", one, path)


def ids_in(body):
    """Every id an answer names, except inside a recorded request path:
    the audit trail quotes what A itself asked for, B's ids included."""
    found = set()

    def walk(v):
        if isinstance(v, dict):
            for k, x in v.items():
                if k != "path":
                    walk(x)
        elif isinstance(v, list):
            for x in v:
                walk(x)
        elif isinstance(v, str):
            found.update(UUID.findall(v))

    walk(body)

    return found


def normalized(status, body):
    return status, UUID.sub("<id>", str(body.get("error", "")))


def snapshot():
    """Everything B holds, read the way B's owner reads it."""
    out = {}
    for method, path in ROUTES:
        if method != "GET" or path.startswith(("/admin", "/projects", "/data")):
            continue

        if "{" in path:
            if family_of(path) is None or path.count("{") > 1:
                continue

            path = substitute(path, True)

        status, body = admin("GET", "/api/v1" + path, None, HB)
        if status == 200:
            out[path] = sorted(set(UUID.findall(json.dumps(body))))

    return out


before = snapshot()
covered, untouched = set(), set()
ASKERS = [
    ("A's * key", lambda m, u, b: anon(m, u, b, HA, kA)),
    ("A's * key naming B", lambda m, u, b: anon(m, u, b, HB, kA)),
    ("A's owner", lambda m, u, b: ua(m, u, b, HA)),
    ("A's owner naming B", lambda m, u, b: ua(m, u, b, HB)),
]
tenant_routes = [
    (m, p) for m, p in ROUTES
    if "{" in p and not p.startswith(("/admin", "/projects", "/invitations"))
]
# Deletes last, so a leak on one cannot hide one on another.
tenant_routes.sort(key=lambda r: r[0] == "DELETE")
for method, path in tenant_routes:
    family = family_of(path)
    if family is None:
        untouched.add(path.split("/{", 1)[0])
        continue

    covered.add(family)
    body = bodies.get(family, {}) if method in ("POST", "PUT", "PATCH", "DELETE") else None
    real, fake = "/api/v1" + substitute(path, True), "/api/v1" + substitute(path, False)
    for who, ask in ASKERS:
        rs, rb = ask(method, real, body)
        fs, fb = ask(method, fake, body)
        note(method, real, rs, rb)
        # A delete of a missing row answers 204, so a 2xx alone proves
        # nothing - what matters is naming B's data, and the snapshot.
        leaked = (b_ids & ids_in(rb)) - set(UUID.findall(real))
        check(not leaked, f"{method} {path}: {who} was shown {len(leaked)} of B's ids")
        check(normalized(rs, rb) == normalized(fs, fb),
              f"{method} {path}: {who} told B's id from a missing one - {rs} {rb.get('error')} vs {fs} {fb.get('error')}")

# Routes addressed by value rather than id, aimed at what B holds.
BY_VALUE = [
    ("DELETE", "/api/v1/suppressions?email=blocked@tenant-b.example", None),
    ("DELETE", "/api/v1/bounces?email=blocked@tenant-b.example", None),
    ("POST", "/api/v1/data/delete-contacts", {"email": "sub@tenant-b.example"}),
    ("POST", "/api/v1/data/delete-email-logs", {"email": "dev@tenant-b.example"}),
]
for method, url, body in BY_VALUE:
    for who, ask in ASKERS:
        ask(method, url, body)

after = snapshot()
for path in sorted(before):
    check(before[path] == after.get(path),
          f"GET {path}: what B holds CHANGED while A was probing it")

# Lists answered to A must name nothing of B's.
list_routes = [(m, p) for m, p in ROUTES if m == "GET" and "{" not in p and not p.startswith("/admin")]
for method, path in list_routes:
    for who, ask in ASKERS:
        status, body = ask(method, "/api/v1" + path, None)
        note(method, path, status, body)
        leaked = b_ids & ids_in(body)
        check(not leaked, f"GET {path}: {who} was shown {len(leaked)} of B's ids")

# Path-addressed project routes: B's project id against one that does not exist.
members = must(admin("GET", f"/api/v1/projects/{B}/members", None, HB), "B members")["members"]
role_b = first_id(must(admin("POST", f"/api/v1/projects/{B}/roles",
                             {"name": "b-role", "permissions": ["templates:read"]}, HB), "B role"))
inv_b = first_id(must(admin("POST", f"/api/v1/projects/{B}/invitations",
                            {"email": "invitee@tenant-b.example"}, HB), "B invitation"))
proj_ids = {"userId": members[0]["user_id"], "roleId": role_b, "invId": inv_b}
proj_routes = [(m, p) for m, p in ROUTES if p.startswith("/projects/{id}")]
proj_routes.sort(key=lambda r: r[0] == "DELETE")
for method, path in proj_routes:
    real = "/api/v1" + re.sub(r"\{([A-Za-z_]+)\}", lambda m: B if m.group(1) == "id" else proj_ids.get(m.group(1), FAKE), path)
    fake = "/api/v1" + re.sub(r"\{[A-Za-z_]+\}", FAKE, path)
    body = {} if method in ("POST", "PUT", "PATCH", "DELETE") else None
    for who, ask in ASKERS:
        rs, rb = ask(method, real, body)
        fs, fb = ask(method, fake, body)
        note(method, real, rs, rb)
        check(rs >= 400, f"{method} {path}: {who} REACHED project B ({rs})")
        check(normalized(rs, rb) == normalized(fs, fb),
              f"{method} {path}: {who} told project B from a missing one - {rs} {rb.get('error')} vs {fs} {fb.get('error')}")

check(must(admin("GET", f"/api/v1/projects/{B}/members", None, HB), "B members after")["members"] == members,
      "project B's membership CHANGED while A was probing it")

print(f"{len(tenant_routes) + len(proj_routes)} id-addressed routes and {len(list_routes)} lists probed across tenants"
      f" ({len(covered)} resource kinds)")
if untouched:
    print("  no fixture, compared on missing ids only: " + ", ".join(sorted(untouched)))

# --- 5. a role cannot be handed out wider than the one who hands it ---
everything = [f"{r}:{a}" for r, actions in catalogue.items() for a in actions]
wide = first_id(must(admin("POST", f"/api/v1/projects/{project}/roles",
                           {"name": "audit-wide", "permissions": everything}, H), "wide role"))
for label, method, url, body in (
    ("invite with", "POST", f"/api/v1/projects/{project}/invitations",
     {"email": "escalate@example.invalid", "role_id": wide}),
    ("make it the default", "PUT", f"/api/v1/projects/{project}/default-role", {"role_id": wide}),
    ("create a role holding everything", "POST", f"/api/v1/projects/{project}/roles",
     {"name": "audit-escalate", "permissions": everything}),
):
    status, b = people["members:write"](method, url, body, H)
    note(method, url, status, b)
    check(status == 403, f"a members:write holder could {label} a wider role ({status})")

print("3 delegation paths probed with a narrower member")

# --- 6. the session surface -------------------------------------------
if APP_SPEC:
    PUBLIC = {
        "/app/api/auth/login", "/app/api/auth/logout", "/app/api/auth/info", "/app/api/auth/register",
        "/app/api/auth/password-reset/request", "/app/api/auth/password-reset/confirm",
        "/app/api/auth/verify-email", "/app/api/auth/verify-email/resend",
        "/app/api/auth/oauth/{slug}/start", "/app/api/auth/oauth/{slug}/callback",
        "/app/api/auth/passkey/login/begin", "/app/api/auth/passkey/login/finish",
    }
    platform = must(admin("POST", "/api/v1/admin/api-keys", {"name": "audit-platform"}), "platform key")
    mya = platform.get("token") or platform.get("key")
    app_routes = all_routes(APP_SPEC)
    for method, path in app_routes:
        url = re.sub(r"\{[A-Za-z_]+\}", FAKE, path)
        body = {} if method in ("POST", "PUT", "PATCH", "DELETE") else None
        if path.startswith("/api/relay-nodes"):
            status, b = anon(method, url, body)
            note(method, url, status, b)
            check(400 <= status < 500, f"{method} {path}: no credential got {status}")
            continue

        if path in PUBLIC:
            continue

        for who, token in (("no credential", None), ("a project key", kA), ("a platform key", mya)):
            status, b = anon(method, url, body, None, token)
            note(method, url, status, b)
            check(status == 401, f"{method} {path}: {who} got {status}, want 401")

    status, _ = ua("GET", "/app/api/events/stats")
    check(status == 403, f"GET /app/api/events/stats: a non-admin got {status}, want 403")
    print(f"{len(app_routes)} session routes probed without a session")

# --- 7. a sandbox credential never reaches real mail ------------------
sbx_a = must(ua("POST", "/api/v1/api-keys",
                {"name": "a-sandbox", "sandbox": True, "permissions": ["sandbox:read", "sandbox:write"]}, HA),
             "A sandbox key")["token"]
email_routes = [(m, p) for m, p in ROUTES
                if p.startswith("/emails") and p not in ("/emails/send", "/emails/send-template")]
for method, path in email_routes:
    url = "/api/v1" + re.sub(r"\{[A-Za-z_]+\}", FAKE, path)
    body = {} if method in ("POST", "PUT", "PATCH", "DELETE") else None
    status, b = anon(method, url, body, HA, sbx_a)
    note(method, url, status, b)
    check(refused(status, b), f"{method} {path}: a sandbox credential got through ({status} {b.get('error')})")

log = must(admin("GET", "/api/v1/emails", None, HB), "B email log")
check(ids["sandbox"] not in json.dumps(log), "a sandbox capture appeared in the real email log")
print(f"{len(email_routes)} email routes probed with a sandbox credential")

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
