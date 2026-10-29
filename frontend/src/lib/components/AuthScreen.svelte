<script>
	import { api } from '#lib/api.js';
	import { t } from '#lib/i18n.js';
	import Icon from './Icon.svelte';

	export let status;
	export let onAuthenticated = () => {};

	let password = '';
	let confirm = '';
	let busy = false;
	let error = '';
	const setup = !status.configured;

	async function submit() {
		error = '';
		if (password.length < 8) {
			error = $t('auth.passwordMin');
			return;
		}
		if (setup && password !== confirm) {
			error = $t('auth.passwordMismatch');
			return;
		}
		busy = true;
		try {
			await api.post(setup ? '/api/auth/setup' : '/api/auth/login', { password });
			await onAuthenticated();
		} catch (err) {
			error = err.message || $t('auth.loginFailed');
		} finally {
			busy = false;
		}
	}
</script>

<div class="auth-screen">
	<div class="card auth-card">
		<div class="auth-brand">
			<div class="brand-mark"><Icon name="zap" size={21} /></div>
			<div>
				<div class="brand-name">FlashRoute</div>
				<div class="brand-tag">{$t('app.tagline')}</div>
			</div>
		</div>
		<h1 class="auth-title">{setup ? $t('auth.setupTitle') : $t('auth.loginTitle')}</h1>
		<p class="auth-sub">{setup ? $t('auth.setupSubtitle') : $t('auth.loginSubtitle')}</p>
		<form class="auth-form" onsubmit={(event) => { event.preventDefault(); submit(); }}>
			<div class="field">
				<label for="auth-password">{$t('auth.password')}<span class="required-mark" aria-hidden="true">*</span></label>
				<input
					id="auth-password"
					class="input"
					type="password"
					bind:value={password}
					autocomplete={setup ? 'new-password' : 'current-password'}
				/>
			</div>
			{#if setup}
				<div class="field">
					<label for="auth-confirm">{$t('auth.confirmPassword')}<span class="required-mark" aria-hidden="true">*</span></label>
					<input id="auth-confirm" class="input" type="password" bind:value={confirm} autocomplete="new-password" />
				</div>
			{/if}
			{#if error}
				<div class="auth-error">{error}</div>
			{/if}
			<button class="btn btn-primary" type="submit" disabled={busy}>
				{#if busy}<span class="spinner"></span>{/if}
				{setup ? $t('auth.setup') : $t('auth.login')}
			</button>
		</form>
	</div>
</div>
