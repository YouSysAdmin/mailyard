<script setup lang="ts">
// Adding or editing one identity provider.
//
// Two hundred lines of form against an eighty-line table, and the form
// is where every rule about an IdP lives - which is why it is its own
// file rather than the back half of a listing page.
//
// THE SECRET IS NEVER READ BACK. The console cannot show it, so an empty
// field on an edit means "leave the stored one alone" and the body
// simply omits it. Sending an empty string instead would blank a working
// provider because somebody renamed it.
import { ref, watch } from 'vue'
import {
  oauthProvidersApi,
  type OAuthProvider,
  type OAuthProviderInput,
} from '../../api/oauthProviders'
import { apiErrorMessage } from '../../api/client'
import { useNotificationStore } from '../../stores/notification'
import { useFieldErrors } from '../../composables/fieldErrors'
import BaseModal from '../../components/BaseModal.vue'
import FormField from '../../components/FormField.vue'

const props = defineProps<{
  /** The provider being edited, or null when adding one. */
  provider: OAuthProvider | null
}>()

const emit = defineEmits<{
  (e: 'saved'): void
  (e: 'close'): void
}>()

const notify = useNotificationStore()
const { errors, capture, clear } = useFieldErrors()

const saving = ref(false)
const showAdvanced = ref(false)

// Kept out of the form's shape unless the operator typed one - see the
// note at the top.
const secretInput = ref('')
const secretTouched = ref(false)

interface Form {
  name: string
  slug: string
  type: string
  client_id: string
  issuer: string
  auth_url: string
  token_url: string
  userinfo_url: string
  scopes: string
  enabled: boolean
  hidden: boolean
  auto_register: boolean
  require_email_verified: boolean
  allowed_domains: string
  allowed_emails: string
  groups_claim: string
  allowed_groups: string
}

function blankForm(): Form {
  return {
    name: '',
    slug: '',
    type: 'oidc',
    client_id: '',
    issuer: '',
    auth_url: '',
    token_url: '',
    userinfo_url: '',
    scopes: '',
    enabled: true,
    hidden: false,
    auto_register: true,
    // On, like the server's own default. Off, an unverified address from
    // the IdP links to the local account carrying that address.
    require_email_verified: true,
    allowed_domains: '',
    allowed_emails: '',
    groups_claim: '',
    allowed_groups: '',
  }
}

const form = ref<Form>(blankForm())

// Lists are edited as comma or newline separated text, which is far less
// fiddly than a tag widget for values pasted out of an IdP console.
function splitList(s: string): string[] {
  return s
    .split(/[\n,]/)
    .map((x) => x.trim())
    .filter(Boolean)
}

function joinList(v: string[] | undefined): string {
  return (v ?? []).join(', ')
}

/** The provider's id, or '' when this is a new one. */
const providerId = ref('')

watch(
  () => props.provider,
  (p) => {
    clear()
    secretInput.value = ''
    secretTouched.value = false

    if (!p) {
      providerId.value = ''
      form.value = blankForm()
      showAdvanced.value = false

      return
    }

    providerId.value = p.id
    form.value = {
      name: p.name,
      slug: p.slug,
      type: p.type,
      client_id: p.client_id,
      issuer: p.issuer,
      auth_url: p.auth_url,
      token_url: p.token_url,
      userinfo_url: p.userinfo_url,
      scopes: joinList(p.scopes),
      enabled: p.enabled,
      hidden: p.hidden,
      auto_register: p.auto_register,
      require_email_verified: p.require_email_verified,
      allowed_domains: joinList(p.allowed_domains),
      allowed_emails: joinList(p.allowed_emails),
      groups_claim: p.groups_claim,
      allowed_groups: joinList(p.allowed_groups),
    }
    // Open Advanced only for the PROTOCOL fields that live there.
    // groups_claim is not one of them - it sits with the access rules
    // now, so including it would open the section for a setting that is
    // no longer inside it.
    showAdvanced.value = Boolean(p.auth_url || p.token_url || p.userinfo_url)
  },
  { immediate: true },
)

function buildBody(): OAuthProviderInput {
  const body: OAuthProviderInput = {
    name: form.value.name.trim(),
    slug: form.value.slug.trim(),
    type: form.value.type,
    client_id: form.value.client_id.trim(),
    issuer: form.value.issuer.trim(),
    auth_url: form.value.auth_url.trim(),
    token_url: form.value.token_url.trim(),
    userinfo_url: form.value.userinfo_url.trim(),
    scopes: splitList(form.value.scopes),
    enabled: form.value.enabled,
    hidden: form.value.hidden,
    auto_register: form.value.auto_register,
    require_email_verified: form.value.require_email_verified,
    allowed_domains: splitList(form.value.allowed_domains),
    allowed_emails: splitList(form.value.allowed_emails),
    groups_claim: form.value.groups_claim.trim(),
    allowed_groups: splitList(form.value.allowed_groups),
  }
  // Only when the operator actually entered one, so an edit of some
  // other field cannot blank it.
  if (secretTouched.value && secretInput.value !== '') {
    body.client_secret = secretInput.value
  }

  return body
}

async function save() {
  clear()
  if (!form.value.name.trim()) return

  saving.value = true
  try {
    if (providerId.value) {
      await oauthProvidersApi.update(providerId.value, buildBody())
      notify.success('Provider updated')
    } else {
      await oauthProvidersApi.create(buildBody())
      notify.success('Provider added')
    }
    emit('saved')
  } catch (e) {
    if (!capture(e)) notify.error(apiErrorMessage(e, 'Failed to save provider'))
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <BaseModal
    :title="provider ? 'Edit Identity Provider' : 'Add Identity Provider'"
    form
    @submit="save"
    @close="emit('close')"
  >
    <FormField label="Name" :error="errors.name" hint="Shown on the sign-in button.">
      <input
        v-model="form.name"
        class="form-input"
        maxlength="100"
        required
        placeholder="Company SSO"
      />
    </FormField>

    <FormField
      label="Type"
      :error="errors.type"
      hint="Google already knows its own endpoints, so it needs only a client id and secret."
    >
      <select v-model="form.type" class="form-select">
        <option value="oidc">OpenID Connect</option>
        <option value="google">Google</option>
      </select>
    </FormField>

    <FormField
      v-if="form.type !== 'google'"
      label="Issuer"
      :error="errors.issuer"
      hint="Endpoints are discovered from this URL. It is the provider's address, not Mailyard's."
    >
      <input
        v-model="form.issuer"
        class="form-input"
        maxlength="400"
        placeholder="https://accounts.example.com"
      />
    </FormField>

    <FormField label="Client ID" :error="errors.client_id">
      <input v-model="form.client_id" class="form-input" maxlength="400" />
    </FormField>
    <FormField label="Client Secret">
      <input
        v-model="secretInput"
        class="form-input"
        type="password"
        maxlength="1000"
        autocomplete="new-password"
        :placeholder="providerId ? 'Leave blank to keep the stored secret' : ''"
        @input="secretTouched = true"
      />
    </FormField>

    <FormField>
      <label class="checkbox-label">
        <input v-model="form.enabled" type="checkbox" />
        <span>Enabled</span>
      </label>
      <label class="checkbox-label">
        <input v-model="form.hidden" type="checkbox" />
        <span>Hidden (works by direct link, no button on the sign-in page)</span>
      </label>
      <label class="checkbox-label">
        <input v-model="form.auto_register" type="checkbox" />
        <span>Create an account on first sign-in</span>
      </label>
      <label class="checkbox-label">
        <input v-model="form.require_email_verified" type="checkbox" />
        <span>Require a verified email address</span>
      </label>
    </FormField>

    <!--
      Who may sign in. All four together and none of them behind
      the advanced toggle: these are the access rules, and three
      of them used to be hidden under settings named for the
      OIDC protocol. An operator restricting a provider to one
      group had to go looking for the control in a section about
      token URLs.
    -->
    <p class="form-section">Who may sign in</p>

    <FormField
      label="Restrict to email domains"
      :error="errors.allowed_domains"
      hint="Comma separated, no @. Leave blank to admit anyone the provider authenticates."
    >
      <input
        v-model="form.allowed_domains"
        class="form-input"
        placeholder="example.com, example.org"
      />
    </FormField>
    <FormField
      label="Restrict to specific addresses"
      :error="errors.allowed_emails"
      hint="When set, only these addresses may sign in and the domain list is ignored."
    >
      <input v-model="form.allowed_emails" class="form-input" placeholder="someone@example.com" />
    </FormField>
    <FormField
      label="Groups claim"
      :error="errors.groups_claim"
      hint="Which claim in the token carries group membership. Providers spell it differently."
    >
      <input
        v-model="form.groups_claim"
        class="form-input"
        placeholder="groups, cognito:groups, roles"
      />
    </FormField>
    <FormField
      label="Restrict to groups"
      :error="errors.allowed_groups"
      hint="Needs a groups claim to be set."
    >
      <input v-model="form.allowed_groups" class="form-input" placeholder="mailyard-admins" />
    </FormField>

    <p class="toggle-advanced" @click="showAdvanced = !showAdvanced">
      {{ showAdvanced ? 'Hide' : 'Show' }} advanced settings
    </p>

    <template v-if="showAdvanced">
      <FormField
        label="Slug"
        :error="errors.slug"
        hint="Appears in the sign-in URL. Changing it changes the redirect URI, which then has to be updated at the provider too."
      >
        <input
          v-model="form.slug"
          class="form-input"
          maxlength="60"
          placeholder="derived from the name"
        />
      </FormField>
      <FormField label="Scopes" :error="errors.scopes">
        <input v-model="form.scopes" class="form-input" placeholder="openid, email, profile" />
      </FormField>
      <FormField
        label="Authorization URL"
        :error="errors.auth_url"
        hint="Only needed for a provider that publishes no discovery document."
      >
        <input v-model="form.auth_url" class="form-input" maxlength="400" />
      </FormField>
      <FormField label="Token URL" :error="errors.token_url">
        <input v-model="form.token_url" class="form-input" maxlength="400" />
      </FormField>
      <FormField label="UserInfo URL" :error="errors.userinfo_url">
        <input v-model="form.userinfo_url" class="form-input" maxlength="400" />
      </FormField>
    </template>
    <template #footer>
      <button type="button" class="btn btn-secondary" @click="emit('close')">Cancel</button>
      <button type="submit" class="btn btn-primary" :disabled="saving || !form.name.trim()">
        {{ saving ? 'Saving...' : providerId ? 'Save' : 'Add Provider' }}
      </button>
    </template>
  </BaseModal>
</template>

<style scoped>
/* The link that opens the protocol fields. Not a button: it reveals
   rather than acts, and a button beside the real ones would read as a
   fourth thing to press. */
.toggle-advanced {
  font-size: 13px;
  color: var(--accent-fg);
  cursor: pointer;
  margin: 8px 0;
}

/* Scoped rather than added to the shared sheet: one form wants a heading
   inside itself, and a design-system class earns its place by being used
   more than once. */
.form-section {
  font-size: 13px;
  font-weight: 600;
  letter-spacing: 0.02em;
  color: var(--text-secondary);
  margin: 20px 0 12px;
  padding-bottom: 6px;
  border-bottom: 1px solid var(--border-secondary);
}
</style>
