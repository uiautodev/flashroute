<script>
	import { onMount } from 'svelte';
	import { api } from '#lib/api.js';
	import { lang, t, toggleLang } from '#lib/i18n.js';
	import { cycleTheme, themeMode } from '#lib/theme.js';
	import Icon from '#lib/components/Icon.svelte';
	import GithubIcon from '#lib/components/GithubIcon.svelte';
	import AuthScreen from '#lib/components/AuthScreen.svelte';
	import ToastStack from '#lib/components/ToastStack.svelte';
	import ChatView from '#lib/components/ChatView.svelte';
	import ProvidersView from '#lib/components/ProvidersView.svelte';
	import RoutesView from '#lib/components/RoutesView.svelte';
	import KeysView from '#lib/components/KeysView.svelte';
	import SecurityView from '#lib/components/SecurityView.svelte';
	import SettingsView from '#lib/components/SettingsView.svelte';
	import AgentsView from '#lib/components/AgentsView.svelte';

	const nav = [
		{ id: 'chat', icon: 'chat', label: 'nav.chat', title: 'chat.title', sub: 'chat.subtitle' },
		{ id: 'providers', icon: 'server', label: 'nav.providers', title: 'providers.title', sub: 'providers.subtitle' },
		{ id: 'routes', icon: 'route', label: 'nav.routes', title: 'routes.title', sub: 'routes.subtitle' },
		{ id: 'keys', icon: 'key', label: 'nav.keys', title: 'keys.title', sub: 'keys.subtitle' },
		{ id: 'security', icon: 'shield', label: 'nav.security', title: 'security.title', sub: 'security.subtitle' },
		{ id: 'agents', icon: 'plug', label: 'nav.agents', title: 'agents.title', sub: 'agents.subtitle' },
		{ id: 'settings', icon: 'settings', label: 'nav.settings', title: 'settings.title', sub: 'settings.subtitle' }
	];

	let status = null;
	let ready = false;
	let loadError = '';
	let view = 'chat';
	let mobileNav = false;

	$: current = nav.find((item) => item.id === view) || nav[0];
	$: themeLabel = $t(`theme.${$themeMode}`);

	onMount(async () => {
		const hash = window.location.hash.replace('#', '');
		if (nav.some((item) => item.id === hash)) view = hash;
		window.addEventListener('hashchange', () => {
			const next = window.location.hash.replace('#', '');
			if (nav.some((item) => item.id === next)) view = next;
		});
		await loadStatus();
	});

	async function loadStatus() {
		loadError = '';
		try {
			status = await api.get('/api/auth/status');
		} catch (err) {
			loadError = err.message;
		} finally {
			ready = true;
		}
	}

	async function authenticated() {
		await loadStatus();
	}

	function navigate(id) {
		view = id;
		mobileNav = false;
		if (window.location.hash !== '#' + id) window.location.hash = id;
	}

	async function logout() {
		try {
			await api.post('/api/auth/logout', {});
		} finally {
			await loadStatus();
		}
	}
</script>

{#if !ready}
	<div class="auth-screen">
		<div class="row" style="color:var(--muted);font-size:13px"><span class="spinner"></span> {$t('common.loading')}</div>
	</div>
{:else if loadError}
	<div class="auth-screen">
		<div class="card auth-card">
			<h1 class="auth-title">{$t('chat.failed')}</h1>
			<p class="auth-sub">{loadError}</p>
			<button class="btn btn-primary" style="margin-top:20px" onclick={loadStatus}>{$t('common.refresh')}</button>
		</div>
	</div>
{:else if status.required && !status.authenticated}
	<AuthScreen {status} onAuthenticated={authenticated} />
{:else}
	<div class="app-shell">
		<div class="scrim" class:show={mobileNav} role="presentation" onclick={() => (mobileNav = false)}></div>
		<aside class="sidebar" class:open={mobileNav}>
			<div class="brand">
				<div class="brand-mark"><Icon name="zap" size={21} /></div>
				<div>
					<div class="brand-name">FlashRoute</div>
					<div class="brand-tag">{$t('app.tagline')}</div>
				</div>
			</div>
			<nav class="nav">
				{#each nav as item}
					<button class="nav-item" class:active={view === item.id} onclick={() => navigate(item.id)}>
						<Icon name={item.icon} size={17} />
						<span>{$t(item.label)}</span>
					</button>
				{/each}
			</nav>
			<div class="sidebar-bottom">
				<div class="mode-chip">
					<span class="mode-dot" class:remote={status.required}></span>
					<span>{status.required ? $t('app.remote') : $t('app.local')}</span>
				</div>
				<div class="mode-chip" style="justify-content:space-between">
					<span>v{status.version}</span>
					{#if status.required && status.authenticated}
						<button class="btn btn-ghost btn-icon" title={$t('app.logout')} onclick={logout}>
							<Icon name="logout" size={15} />
						</button>
					{/if}
				</div>
			</div>
		</aside>

		<div class="main">
			<header class="topbar">
				<div class="row">
					<button class="btn btn-ghost btn-icon mobile-menu" onclick={() => (mobileNav = true)} aria-label="Menu">
						<Icon name="menu" size={18} />
					</button>
					<div>
						<div class="topbar-title">{$t(current.title)}</div>
						<div class="topbar-sub">{$t(current.sub)}</div>
					</div>
				</div>
				<div class="topbar-actions">
					<a
						class="btn btn-ghost btn-icon"
						href="https://github.com/uiautodev/flashroute"
						target="_blank"
						rel="noreferrer"
						title="GitHub"
						aria-label="GitHub"
						style="text-decoration:none"
					>
						<GithubIcon size={16} />
					</a>
					<button
						class="btn btn-ghost btn-icon"
						onclick={cycleTheme}
						title={$t('theme.current', { mode: themeLabel })}
						aria-label={$t('theme.current', { mode: themeLabel })}
					>
						{#if $themeMode === 'dark'}
							<Icon name="moon" size={16} />
						{:else if $themeMode === 'light'}
							<Icon name="sun" size={16} />
						{:else}
							<Icon name="monitor" size={16} />
						{/if}
					</button>
					<button class="btn btn-ghost btn-sm" onclick={toggleLang} title={$t('common.language')}>
						<Icon name="globe" size={15} />
						<span>{$lang === 'zh' ? 'EN' : '中文'}</span>
					</button>
				</div>
			</header>

			<main class="content" class:chat-content={view === 'chat'}>
				{#if view === 'chat'}
					<ChatView onNavigate={navigate} />
				{:else if view === 'providers'}
					<ProvidersView />
				{:else if view === 'routes'}
					<RoutesView />
				{:else if view === 'keys'}
					<KeysView />
				{:else if view === 'security'}
					<SecurityView />
				{:else if view === 'agents'}
					<AgentsView />
				{:else if view === 'settings'}
					<SettingsView {status} onLogout={logout} />
				{/if}
			</main>
		</div>
	</div>
{/if}

<ToastStack />
