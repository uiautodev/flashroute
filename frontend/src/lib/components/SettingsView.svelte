<script>
	import { api } from '#lib/api.js';
	import { lang, t } from '#lib/i18n.js';
	import { toast } from '#lib/toast.js';
	import { themeChoices, themeMode } from '#lib/theme.js';
	import Icon from './Icon.svelte';
	import CopyButton from './CopyButton.svelte';

	export let status;
	export let onLogout = () => {};

	let password = '';
	let confirm = '';
	let saving = false;
	let checkingUpdate = false;
	let updating = false;
	let applied = false;
	let update = null;
	let updateError = '';

	$: endpoint = typeof location !== 'undefined' ? location.origin + '/v1' : '/v1';

	async function updatePassword() {
		if (password.length < 8) {
			toast($t('auth.passwordMin'), 'error');
			return;
		}
		if (password !== confirm) {
			toast($t('auth.passwordMismatch'), 'error');
			return;
		}
		saving = true;
		try {
			await api.post('/api/auth/password', { password });
			password = '';
			confirm = '';
			toast($t('settings.passwordUpdated'), 'success');
		} catch (err) {
			toast(err.message, 'error');
		} finally {
			saving = false;
		}
	}

	async function checkForUpdate() {
		checkingUpdate = true;
		updateError = '';
		applied = false;
		try {
			update = await api.get('/api/update/check');
		} catch (err) {
			update = null;
			updateError = err.message;
		} finally {
			checkingUpdate = false;
		}
	}

	async function installUpdate() {
		if (!update?.available || updating) return;
		updating = true;
		updateError = '';
		try {
			await api.post('/api/update/apply', {});
			update = null;
			applied = true;
			toast($t('settings.updateRestarting'), 'success');
			await waitForRestart();
		} catch (err) {
			updateError = err.message;
			applied = false;
		} finally {
			updating = false;
		}
	}

	async function waitForRestart() {
		await new Promise((resolve) => setTimeout(resolve, 1500));
		for (let attempt = 0; attempt < 60; attempt++) {
			try {
				await api.get('/api/auth/status');
				location.reload();
				return;
			} catch {
				await new Promise((resolve) => setTimeout(resolve, 1000));
			}
		}
		applied = false;
		updateError = $t('settings.updateRestartTimeout');
	}
</script>

<div class="settings-grid">
	<section class="card card-pad">
		<div class="section-head">
			<div class="section-icon"><Icon name="shield" size={17} /></div>
			<div>
				<h2>{$t('settings.authTitle')}</h2>
				<p>{status.required ? $t('settings.authRemote') : $t('settings.authLocal')}</p>
			</div>
		</div>
		<div class="auth-row">
			<span class="badge {status.required ? 'badge-warn' : 'badge-ok'}">
				{status.required ? $t('app.remote') : $t('app.local')}
			</span>
			{#if status.configured}
				<span class="badge badge-off">{$t('auth.password')}: {$t('common.yes')}</span>
			{:else}
				<span class="badge badge-off">{$t('auth.password')}: {$t('common.no')}</span>
			{/if}
		</div>
		<div class="divider"></div>
		<form class="password-form" onsubmit={(event) => { event.preventDefault(); updatePassword(); }}>
			<div class="field">
				<label for="new-password">{$t('settings.newPassword')}<span class="required-mark" aria-hidden="true">*</span></label>
				<input id="new-password" class="input" type="password" bind:value={password} autocomplete="new-password" placeholder="••••••••" />
			</div>
			<div class="field">
				<label for="confirm-password">{$t('auth.confirmPassword')}<span class="required-mark" aria-hidden="true">*</span></label>
				<input id="confirm-password" class="input" type="password" bind:value={confirm} autocomplete="new-password" placeholder="••••••••" />
			</div>
			<button class="btn btn-primary" type="submit" disabled={saving || !password}>
				{#if saving}<span class="spinner"></span>{/if}
				{$t('settings.updatePassword')}
			</button>
		</form>
		{#if status.required && status.authenticated}
			<div class="divider"></div>
			<button class="btn btn-danger" onclick={onLogout}><Icon name="logout" size={14} />{$t('settings.logout')}</button>
		{/if}
		<p class="hint force-hint">{$t('settings.forceAuthHint')}</p>
	</section>

	<section class="card card-pad">
		<div class="section-head">
			<div class="section-icon"><Icon name="server" size={17} /></div>
			<div>
				<h2>{$t('settings.mode')} & {$t('settings.dataDir')}</h2>
				<p>{$t('settings.aboutBody')}</p>
			</div>
		</div>
		<div class="divider"></div>
		<dl class="kv">
			<dt>{$t('settings.version')}</dt>
			<dd class="mono">
				v{status.version}
				{#if status.commit && status.commit !== 'unknown'}
					<a class="commit-link" href={`https://github.com/uiautodev/flashroute/commit/${status.commit}`} target="_blank" rel="noreferrer">
						{status.commit.slice(0, 7)}
					</a>
				{/if}
			</dd>
			<dt>{$t('settings.mode')}</dt>
			<dd>{status.desktop ? $t('settings.desktop') : $t('settings.server')}</dd>
			<dt>{$t('settings.dataDir')}</dt>
			<dd class="mono">{status.data_dir}</dd>
			<dt>{$t('settings.gateway')}</dt>
			<dd>
				<span class="inline-copy">
					<code class="mono">{endpoint}</code>
					<CopyButton value={endpoint} />
				</span>
			</dd>
			<dt>{$t('settings.language')}</dt>
			<dd>
				<select class="select" style="max-width:190px" bind:value={$lang}>
					<option value="zh">中文</option>
					<option value="en">English</option>
				</select>
			</dd>
		</dl>
	</section>

	<section class="card card-pad appearance-card">
		<div class="section-head">
			<div class="section-icon"><Icon name="sun" size={17} /></div>
			<div>
				<h2>{$t('settings.appearanceTitle')}</h2>
				<p>{$t('settings.appearanceHint')}</p>
			</div>
		</div>
		<div class="divider"></div>
		<div class="theme-options" role="group" aria-label={$t('theme.label')}>
			{#each themeChoices as choice}
				<button
					class="theme-option"
					class:active={$themeMode === choice.id}
					onclick={() => themeMode.set(choice.id)}
					aria-pressed={$themeMode === choice.id}
				>
					<Icon name={choice.icon} size={17} />
					<span>{$t('theme.' + choice.id)}</span>
					{#if $themeMode === choice.id}<Icon name="check" size={14} />{/if}
				</button>
			{/each}
		</div>
	</section>

	<section class="card card-pad about-card">
		<div class="section-head">
			<div class="section-icon"><Icon name="zap" size={17} /></div>
			<div>
				<h2>{$t('settings.aboutTitle')}</h2>
				<p>{$t('settings.aboutBody')}</p>
			</div>
		</div>
	</section>

	<section class="card card-pad update-card">
		<div class="section-head">
			<div class="section-icon"><Icon name="refresh" size={17} /></div>
			<div>
				<h2>{$t('settings.updateTitle')}</h2>
				<p>{$t('settings.updateHint')}</p>
			</div>
		</div>
		<div class="divider"></div>
		<div class="update-row">
			<div class="update-versions">
				<span class="badge">v{status.version}</span>
				{#if update?.available && update.latest}
					<span class="update-arrow" aria-hidden="true">→</span>
					<span class="badge badge-ok">v{update.latest}</span>
				{/if}
			</div>
			<button class="btn btn-sm" onclick={checkForUpdate} disabled={checkingUpdate || updating || applied}>
				{#if checkingUpdate}<span class="spinner"></span>{:else}<Icon name="refresh" size={13} />{/if}
				{checkingUpdate ? $t('settings.updateChecking') : $t('settings.updateCheck')}
			</button>
		</div>
		{#if updateError}
			<p class="update-error">{updateError}</p>
		{/if}
		{#if applied}
			<p class="update-restarting"><span class="spinner"></span>{$t('settings.updateRestarting')}</p>
		{:else if update}
			{#if update.available}
				<div class="update-available">
					<span class="badge badge-warn">{$t('settings.updateAvailable')}</span>
					{#if update.notes}
						<pre class="release-notes">{update.notes}</pre>
					{/if}
					<div class="update-actions">
						<button class="btn btn-primary" onclick={installUpdate} disabled={updating}>
							{#if updating}<span class="spinner"></span>{:else}<Icon name="download" size={14} />{/if}
							{updating ? $t('settings.updateDownloading') : $t('settings.updateInstall')}
						</button>
						{#if update.release_url}
							<a class="btn btn-ghost btn-sm" href={update.release_url} target="_blank" rel="noreferrer">
								<Icon name="external" size={13} />{$t('settings.updateRelease')}
							</a>
						{/if}
					</div>
				</div>
			{:else}
				<p class="hint">{update.dev ? $t('settings.updateDev') : $t('settings.updateUpToDate')}</p>
			{/if}
		{/if}
	</section>
</div>

<style>
	.commit-link {
		margin-left: 6px;
		color: var(--accent);
		text-decoration: none;
	}
	.commit-link:hover {
		text-decoration: underline;
	}
	.settings-grid {
		display: grid;
		grid-template-columns: repeat(2, minmax(0, 1fr));
		gap: 18px;
		align-items: start;
	}
	.section-head {
		display: flex;
		gap: 11px;
		align-items: flex-start;
	}
	.section-icon {
		width: 34px;
		height: 34px;
		border-radius: 10px;
		display: grid;
		place-items: center;
		background: var(--accent-soft);
		color: var(--accent);
		border: 1px solid rgba(52, 211, 153, 0.14);
		flex: none;
	}
	.section-head h2 {
		font-size: 14px;
		font-weight: 680;
	}
	.section-head p {
		font-size: 11.5px;
		color: var(--muted);
		line-height: 1.55;
		margin-top: 4px;
	}
	.auth-row {
		display: flex;
		gap: 8px;
		margin-top: 14px;
		flex-wrap: wrap;
	}
	.password-form {
		display: grid;
		grid-template-columns: repeat(2, minmax(0, 1fr));
		gap: 12px;
		align-items: end;
	}
	.password-form .btn {
		grid-column: 1 / -1;
		justify-self: start;
	}
	.force-hint {
		margin-top: 14px;
	}
	.inline-copy {
		display: inline-flex;
		align-items: center;
		gap: 4px;
		max-width: 100%;
	}
	.inline-copy code {
		font-size: 11px;
		overflow-wrap: anywhere;
	}
	.about-card {
		grid-column: 1 / -1;
	}
	.update-card {
		grid-column: 1 / -1;
	}
	.update-row {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 12px;
		flex-wrap: wrap;
	}
	.update-versions {
		display: flex;
		align-items: center;
		gap: 8px;
	}
	.update-arrow {
		color: var(--muted);
		font-size: 12px;
	}
	.update-available {
		display: flex;
		flex-direction: column;
		gap: 10px;
		margin-top: 14px;
		align-items: flex-start;
	}
	.update-actions {
		display: flex;
		align-items: center;
		gap: 8px;
		flex-wrap: wrap;
	}
	.release-notes {
		width: 100%;
		max-height: 220px;
		overflow: auto;
		margin: 0;
		padding: 12px 14px;
		border: 1px solid var(--line-soft);
		border-radius: 9px;
		background: var(--surface-inset);
		color: var(--text-soft);
		font-family: inherit;
		font-size: 11.5px;
		line-height: 1.6;
		white-space: pre-wrap;
		overflow-wrap: anywhere;
	}
	.update-error {
		margin-top: 12px;
		font-size: 11.5px;
		line-height: 1.5;
		color: var(--danger-text);
		overflow-wrap: anywhere;
	}
	.update-restarting {
		display: flex;
		align-items: center;
		gap: 8px;
		margin-top: 14px;
		font-size: 12px;
		color: var(--muted);
	}
	.appearance-card {
		grid-column: 1 / -1;
	}
	.theme-options {
		display: grid;
		grid-template-columns: repeat(3, minmax(0, 1fr));
		gap: 9px;
	}
	.theme-option {
		display: flex;
		align-items: center;
		justify-content: center;
		gap: 8px;
		padding: 11px 12px;
		border: 1px solid var(--line);
		border-radius: 9px;
		background: var(--surface-inset);
		color: var(--muted);
		font-size: 12px;
		font-weight: 600;
		transition:
			background 0.15s ease,
			border-color 0.15s ease,
			color 0.15s ease;
	}
	.theme-option:hover {
		background: var(--hover-tint);
		color: var(--text);
	}
	.theme-option.active {
		border-color: rgba(52, 211, 153, 0.4);
		background: var(--accent-soft);
		color: var(--accent);
	}
	@media (max-width: 900px) {
	.settings-grid {
			grid-template-columns: 1fr;
		}
		.password-form {
			grid-template-columns: 1fr;
		}
	}
</style>
