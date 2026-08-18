"""The hand-written half of the Python client.

Small on purpose: a transport, an error type, and cursor paging. The
route surface is generated next door in api.py, because two hundred
routes written by hand is how a client falls behind its server.

Standard library only. A mail SDK that drags in a HTTP stack is a
dependency an operator has to audit, and urllib does what this needs.
"""

from __future__ import annotations

import json
import urllib.error
import urllib.parse
import urllib.request
from typing import Any, Iterator, Mapping, Optional

from .api import API

DEFAULT_BASE_URL = "http://localhost:3000"
USER_AGENT = "mailyard-python"


class _NoRedirects(urllib.request.HTTPRedirectHandler):
    """Refuses redirects instead of following them with the API key.

    urllib's default handler copies every request header onto the
    redirect target, Authorization included - so a 302 to another host
    handed `Bearer myk_...` to whoever answered it. The Go client strips
    the header across hosts and Ruby's Net::HTTP does not follow
    redirects at all, so this was the one client that leaked.

    Refusing rather than re-signing, because this API does not redirect:
    every /api/v1 route answers directly. A 3xx here means something is
    in front of the server that the caller should know about - a proxy,
    a captive portal, or a plain http:// base URL being bounced to
    https:// - and the fix is the base URL, not a silent second request.
    """

    def redirect_request(self, req, fp, code, msg, headers, newurl):
        raise MailyardError(
            code,
            "refusing to follow a redirect to " + newurl
            + " - set base_url to the API's real address",
        )


class MailyardError(Exception):
    """An error the API reported.

    Carries the status and the field list the server sent, so a caller
    can tell a validation failure from a refusal without parsing the
    message.
    """

    def __init__(self, status: int, message: str, fields: Optional[list] = None):
        super().__init__(f"{status}: {message}")
        self.status = status
        self.message = message
        self.fields = fields or []

    @property
    def is_validation(self) -> bool:
        return self.status == 400

    @property
    def is_forbidden(self) -> bool:
        return self.status == 403

    @property
    def is_rate_limited(self) -> bool:
        return self.status == 429


class Client:
    """A Mailyard API client.

        client = Client(api_key="myk_...")
        client.api.send_email(body={"from": "a@b.com", "to": ["c@d.com"],
                                    "subject": "Hi", "text": "Hello"})

    The key names its project, so there is no project to pass.
    """

    def __init__(
        self,
        api_key: str,
        base_url: str = DEFAULT_BASE_URL,
        timeout: float = 30.0,
    ):
        if not api_key:
            raise ValueError("an api key is required")
        self.api_key = api_key
        self.base_url = base_url.rstrip("/")
        self.timeout = timeout
        self.api = API(self)
        # Built once per client, not per request: an opener carries no
        # per-request state and building one each time would also rebuild
        # the proxy lookup.
        self._opener = urllib.request.build_opener(_NoRedirects)

    # --- transport ---

    def request(
        self,
        method: str,
        path: str,
        body: Optional[Mapping[str, Any]] = None,
        query: Optional[Mapping[str, Any]] = None,
        raw: bool = False,
    ) -> Any:
        """Perform one request.

        raw=True returns the response BYTES undecoded, for the routes
        that answer a raw message or a decoded attachment. Those used to
        be parsed as JSON like everything else, so json.loads raised a
        bare ValueError on an RFC 5322 message - an exception type a
        caller catching MailyardError never sees.
        """
        url = self.base_url + "/api/v1" + path
        clean = {k: v for k, v in (query or {}).items() if v is not None}
        if clean:
            url += "?" + urllib.parse.urlencode(clean, doseq=True)

        data = None
        if body is not None:
            data = json.dumps(body).encode()

        req = urllib.request.Request(url, data=data, method=method)
        req.add_header("Authorization", "Bearer " + self.api_key)
        req.add_header("User-Agent", USER_AGENT)
        if data is not None:
            req.add_header("Content-Type", "application/json")

        try:
            with self._opener.open(req, timeout=self.timeout) as resp:
                payload = resp.read()
                if raw:
                    return payload
                if not payload:
                    return None
                return json.loads(payload)
        except urllib.error.HTTPError as e:
            raw = e.read()
            try:
                payload = json.loads(raw) if raw else {}
            except ValueError:
                payload = {}
            raise MailyardError(
                e.code,
                payload.get("error", e.reason or "request failed"),
                payload.get("fields"),
            ) from None

    # --- paging ---

    def paginate(self, method_name: str, *args, key: str, **query) -> Iterator[dict]:
        """Walk a cursor-paged list, yielding rows.

        The logs - emails, bounces, suppressions, webhook deliveries -
        page by cursor rather than offset, so there is no page count to
        loop over and no total to read.

            for email in client.paginate("list_emails", key="emails"):
                ...

        Path parameters are passed positionally, exactly as they are to
        the method itself:

            for m in client.paginate(
                "list_subscriber_list_members", list_id, key="members"
            ):
                ...

        The signature accepted *args and then dropped them, so any
        paged list with a path parameter raised TypeError about a
        missing argument - blaming the generated method rather than
        this call.
        """
        call = getattr(self.api, method_name)
        cursor = None
        while True:
            page = call(*args, **{**query, **({"cursor": cursor} if cursor else {})})
            rows = page.get(key) or []
            for row in rows:
                yield row
            cursor = page.get("next_cursor")
            if not cursor:
                return
