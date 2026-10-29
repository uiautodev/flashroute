<script>
	import { onMount } from 'svelte';
	import { api } from '#lib/api.js';
	import { t } from '#lib/i18n.js';
	import { toast } from '#lib/toast.js';
	import Icon from './Icon.svelte';
	import CopyButton from './CopyButton.svelte';
	import ProviderBalance from './ProviderBalance.svelte';
	import Modal from './Modal.svelte';

	const presets = [
		{ id: 'ds', name: 'DeepSeek', base_url: 'https://api.deepseek.com/v1' },
		{ id: 'openrouter', name: 'OpenRouter', base_url: 'https://openrouter.ai/api/v1' },
		{ id: 'bigmodel', name: 'BigModel', base_url: 'https://open.bigmodel.cn/api/paas/v4' }
	];

	let providers = [];
	let loading = true;
	let error = '';
	let showModal = false;
	let editing = false;
	let saving = false;
	let testing = false;
	let syncing = false;
	let proxyTesting = false;
	let proxyResult = null;
	let keyFocused = false;
	let form = blankForm();
	let modelText = '';
	let availableModels = [];
	let testingModel = '';
	let expanded = {};
	let viewMode = 'cards';
	let modelTab = 'list';

	function blankForm() {
		return { id: '', name: '', base_url: '', api_type: 'completions', api_key: '', proxy: '', enabled: true, models: [] };
	}

	onMount(() => {
		if (typeof localStorage !== 'undefined') {
			const saved = localStorage.getItem('flashroute:providers-view');
			if (saved === 'cards' || saved === 'list') viewMode = saved;
		}
		load();
	});

	function setView(mode) {
		viewMode = mode;
		try {
			localStorage.setItem('flashroute:providers-view', mode);
		} catch {
			/* ignore */
		}
	}

	function supportsBalance(provider) {
		return /deepseek\.com|openrouter\.ai/i.test(provider.base_url || '');
	}

	function apiTypeLabel(provider) {
		const type = provider.api_type || 'completions';
		if (type === 'responses') return 'Responses';
		if (type === 'anthropic') return 'Anthropic';
		return 'Completions';
	}

	async function load() {
		loading = true;
		error = '';
		try {
			const data = await api.get('/api/providers');
			providers = data.providers || [];
		} catch (err) {
			error = err.message;
		} finally {
			loading = false;
		}
	}

	function openAdd() {
		editing = false;
		form = blankForm();
		modelText = '';
		availableModels = [];
		modelTab = 'list';
		proxyResult = null;
		keyFocused = false;
		showModal = true;
	}

	function openEdit(provider) {
		editing = true;
		form = { ...provider, api_type: provider.api_type || 'completions', models: provider.models || [] };
		modelText = (provider.models || []).map((m) => m.id).join('\n');
		availableModels = [...(provider.models || [])];
		const models = provider.models || [];
		const selected = new Set(models.map((model) => model.id));
		modelTab = selected.size === models.length ? 'list' : 'selected';
		proxyResult = null;
		keyFocused = false;
		showModal = true;
	}

	function applyPreset(preset) {
		form = { ...form, id: preset.id, name: preset.name, base_url: preset.base_url, api_type: 'completions', enabled: true };
	}

	function parseModels(text) {
		const seen = new Set();
		const out = [];
		for (const line of text.split('\n')) {
			const id = line.trim();
			if (!id || seen.has(id)) continue;
			seen.add(id);
			out.push({ id, name: id });
		}
		return out;
	}

	async function save() {
		saving = true;
		try {
			const payload = { ...form, models: selectedModelObjects() };
			await api.post('/api/providers', payload);
			showModal = false;
			toast($t('common.save'), 'success');
			await load();
		} catch (err) {
			toast(err.message, 'error');
		} finally {
			saving = false;
		}
	}

	async function testConnection() {
		testing = true;
		try {
			const result = await api.post('/api/providers/test', {
				...form,
				models: parseModels(modelText)
			});
			if (result.models?.length) {
				availableModels = result.models;
				if (modelTab === 'list') modelText = availableModels.map((model) => model.id).join('\n');
			}
			if (result.ok) {
				toast($t('providers.connectionOk', { ms: result.latency_ms, count: result.models_count }), 'success', 5000);
			} else {
				toast(result.error || $t('providers.connectionFailed'), 'error', 5000);
			}
		} catch (err) {
			toast(err.message, 'error', 5000);
		} finally {
			testing = false;
		}
	}

	async function syncModels() {
		if (!form.base_url) return;
		syncing = true;
		try {
			const result = await api.post('/api/providers/test', {
				...form,
				models: parseModels(modelText),
				test_model: false
			});
			if (!result.ok) {
				toast(result.error || $t('providers.connectionFailed'), 'error', 5000);
				return;
			}
			availableModels = result.models || [];
			if (modelTab === 'list') modelText = availableModels.map((model) => model.id).join('\n');
			toast($t('providers.modelsSyncedCandidates', { count: result.models_count ?? availableModels.length }), 'success');
		} catch (err) {
			toast(err.message, 'error', 5000);
		} finally {
			syncing = false;
		}
	}

	async function testProxy() {
		if (!form.proxy || !form.base_url) return;
		proxyTesting = true;
		proxyResult = null;
		try {
			proxyResult = await api.post('/api/proxy/test', { proxy: form.proxy, url: form.base_url });
			if (!proxyResult.ok) toast(proxyResult.error || $t('providers.proxyFailed'), 'error', 5000);
		} catch (err) {
			proxyResult = { ok: false, error: err.message };
			toast(err.message, 'error', 5000);
		} finally {
			proxyTesting = false;
		}
	}

	async function refreshModels(provider) {
		openEdit(provider);
		await syncModels();
	}

	function buildModelCatalog(available, selected) {
		const seen = new Set();
		const out = [];
		for (const model of [...available, ...selected]) {
			if (!model?.id || seen.has(model.id)) continue;
			seen.add(model.id);
			out.push(model);
		}
		return out;
	}

	function toggleModel(model) {
		const ids = new Set(selectedIDs);
		if (ids.has(model.id)) ids.delete(model.id);
		else ids.add(model.id);
		modelText = [...ids].join('\n');
		if (ids.size === modelCatalog.length) modelTab = 'list';
		else if (ids.size === 0) modelTab = 'usage';
		else modelTab = 'selected';
	}

	function selectAllModels() {
		modelText = modelCatalog.map((model) => model.id).join('\n');
		modelTab = 'list';
	}

	function selectTab(tab) {
		if (tab === 'list') {
			selectAllModels();
			return;
		}
		if (tab === 'usage') modelText = '';
		modelTab = tab;
	}

	function selectedModelObjects() {
		const known = new Map(modelCatalog.map((model) => [model.id, model]));
		return selectedIDs.map((id) => {
			const model = known.get(id);
			return model ? { ...model } : { id, name: id };
		});
	}

	function modelStatus(model) {
		if (model.last_test) return model.last_test.ok ? 'ok' : 'bad';
		return selectedIDs.includes(model.id) ? 'selected' : 'idle';
	}

	function toggleExpand(id) {
		expanded = { ...expanded, [id]: !expanded[id] };
	}

	function maskKey(value) {
		const key = String(value || '');
		if (!key) return '';
		if (key.length <= 10) return key.slice(0, 2) + '****' + key.slice(-2);
		return key.slice(0, 6) + '****' + key.slice(-4);
	}

	$: selectedModels = parseModels(modelText);
	$: selectedIDs = selectedModels.map((model) => model.id);
	$: modelCatalog = buildModelCatalog(availableModels, selectedModels);
	$: apiEndpoint = form.base_url
		? form.base_url.replace(/\/+$/, '') +
			(form.api_type === 'responses' ? '/responses' : form.api_type === 'anthropic' ? '/messages' : '/chat/completions')
		: '';
</script>

<div class="page-toolbar">
	<div class="view-toggle">
		<button class="btn btn-sm" class:active={viewMode === 'cards'} onclick={() => setView('cards')} title={$t('providers.viewCards')} aria-label={$t('providers.viewCards')}>
			<Icon name="grid" size={14} />
		</button>
		<button class="btn btn-sm" class:active={viewMode === 'list'} onclick={() => setView('list')} title={$t('providers.viewList')} aria-label={$t('providers.viewList')}>
			<Icon name="list" size={14} />
		</button>
	</div>
	<button class="btn btn-primary" onclick={openAdd}><Icon name="plus" size={15} />{$t('providers.add')}</button>
</div>

{#if loading}
	<div class="grid">
		<div class="card card-pad"><div class="skeleton" style="width:35%"></div></div>
		<div class="card card-pad"><div class="skeleton" style="width:50%"></div></div>
	</div>
{:else if error}
	<div class="empty"><span class="empty-text">{error}</span><button class="btn" onclick={load}>{$t('common.refresh')}</button></div>
{:else if providers.length === 0}
	<div class="empty">
		<div class="empty-icon"><Icon name="server" size={23} /></div>
		<h2 class="empty-title">{$t('providers.emptyTitle')}</h2>
		<p class="empty-text">{$t('providers.emptyBody')}</p>
		<div class="preset-row">
			{#each presets as preset}
				<button
					class="btn"
					onclick={() => {
						form = blankForm();
						applyPreset(preset);
						editing = false;
						showModal = true;
					}}>{$t('providers.add')} {preset.name}</button
				>
			{/each}
		</div>
		<button class="btn btn-primary" onclick={openAdd}>{$t('providers.custom')}</button>
	</div>
{:else}
	{#if viewMode === 'list'}
		<div class="card provider-list-card">
			<table class="provider-table">
				<thead>
					<tr>
						<th>{$t('providers.title')}</th>
						<th>{$t('providers.models')}</th>
						<th>{$t('providers.balance')}</th>
						<th></th>
					</tr>
				</thead>
				<tbody>
					{#each providers as provider (provider.id)}
						<tr class:disabled={!provider.enabled}>
							<td>
								<div class="list-identity">
									<div class="provider-avatar small">{provider.name.slice(0, 1).toUpperCase()}</div>
									<div class="list-names">
										<div class="list-title">
											<span>{provider.name}</span>
											<span class="badge {provider.enabled ? 'badge-ok' : 'badge-off'}">
												{provider.enabled ? $t('common.enabled') : $t('common.disabled')}
											</span>
											<span class="badge">{apiTypeLabel(provider)}</span>
										</div>
										<div class="mono list-sub">{provider.id} · {provider.base_url}</div>
									</div>
								</div>
							</td>
							<td class="list-models">{(provider.models || []).length}</td>
							<td>
								{#if supportsBalance(provider)}
									<ProviderBalance {provider} />
								{:else}
									<span class="hint">—</span>
								{/if}
							</td>
							<td>
								<div class="row list-actions">
									<button class="switch" class:on={provider.enabled} onclick={() => toggleProvider(provider)} title={$t('providers.enabledHint')}></button>
									<button class="btn btn-ghost btn-icon" onclick={() => openEdit(provider)} title={$t('common.edit')}><Icon name="edit" size={15} /></button>
									<button class="btn btn-ghost btn-icon" onclick={() => remove(provider)} title={$t('common.delete')}><Icon name="trash" size={15} /></button>
								</div>
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	{:else}
	<div class="provider-grid">
		{#each providers as provider (provider.id)}
			<article class="card provider-card" class:disabled={!provider.enabled}>
				<header class="provider-head">
					<div class="provider-identity">
						<div class="provider-avatar">{provider.name.slice(0, 1).toUpperCase()}</div>
						<div class="provider-names">
							<div class="provider-title-row">
								<h2>{provider.name}</h2>
								<span class="badge {provider.enabled ? 'badge-ok' : 'badge-off'}">
									{provider.enabled ? $t('common.enabled') : $t('common.disabled')}
								</span>
								<span class="badge">{(provider.api_type || 'completions') === 'responses' ? 'Responses' : 'Completions'}</span>
							</div>
							<div class="provider-id mono">{provider.id}</div>
						</div>
					</div>
					<div class="row">
						<button class="switch" class:on={provider.enabled} onclick={() => toggleProvider(provider)} title={$t('providers.enabledHint')}></button>
						<button class="btn btn-ghost btn-icon" onclick={() => openEdit(provider)} title={$t('common.edit')}><Icon name="edit" size={15} /></button>
						<button class="btn btn-ghost btn-icon" onclick={() => remove(provider)} title={$t('common.delete')}><Icon name="trash" size={15} /></button>
					</div>
				</header>

				<div class="provider-details">
					<div class="detail">
						<span class="detail-label">{$t('providers.baseUrl')}</span>
						<span class="mono detail-value">{provider.base_url}</span>
					</div>
					{#if supportsBalance(provider)}
						<div class="detail">
							<span class="detail-label">{$t('providers.balance')}</span>
							<ProviderBalance {provider} />
						</div>
					{/if}
					<div class="detail">
						<span class="detail-label">{$t('providers.apiKey')}</span>
						{#if provider.api_key}
							<span class="key-row">
								<code class="key-value" aria-label={provider.api_key}>{maskKey(provider.api_key)}</code>
								<CopyButton value={provider.api_key} />
							</span>
						{:else}
							<span class="badge badge-warn">{$t('providers.missingKey')}</span>
						{/if}
					</div>
					{#if provider.proxy}
						<div class="detail">
							<span class="detail-label">{$t('providers.proxy')}</span>
							<span class="mono detail-value">{provider.proxy}</span>
						</div>
					{/if}
				</div>

				<div class="models-head">
					<div class="row" style="gap:8px">
						<span class="detail-label">{$t('providers.models')}</span>
						<span class="badge">{(provider.models || []).length}</span>
						{#if (provider.models || []).length}
							<button class="btn btn-ghost btn-icon btn-sm" onclick={() => toggleExpand(provider.id)} title={expanded[provider.id] ? $t('common.close') : $t('common.models')}>
								<Icon name={expanded[provider.id] ? 'chevron-up' : 'chevron-down'} size={14} />
							</button>
						{/if}
					</div>
					<button class="btn btn-sm" onclick={() => refreshModels(provider)}>
						<Icon name="refresh" size={13} />
						{$t('providers.refreshModels')}
					</button>
				</div>

				{#if (provider.models || []).length}
					<div class="model-list" class:expanded={expanded[provider.id]}>
						{#each expanded[provider.id] ? provider.models : provider.models.slice(0, 4) as model (model.id)}
							<div class="model-row">
								<div class="model-name">
									<span class="mono model-id">{model.id}</span>
									{#if model.name && model.name !== model.id}<span class="model-label">{model.name}</span>{/if}
									{#if model.last_test}
										<span class="badge {model.last_test.ok ? 'badge-ok' : 'badge-bad'}">
											{model.last_test.ok ? $t('providers.testOk', { output: model.last_test.output, ms: model.last_test.latency_ms }) : $t('providers.testBad')}
										</span>
										{#if !model.last_test.ok && model.last_test.error}
											<span class="model-test-error">{model.last_test.error}</span>
										{/if}
									{/if}
								</div>
								<button
									class="btn btn-sm"
									onclick={() => testModel(provider, model)}
									disabled={testingModel === provider.id + '/' + model.id}
								>
									{#if testingModel === provider.id + '/' + model.id}<span class="spinner"></span>{:else}<Icon name="zap" size={12} />{/if}
									{$t('providers.testModel')}
								</button>
							</div>
						{/each}
						{#if (provider.models || []).length > 4 && !expanded[provider.id]}
							<button class="more-models" onclick={() => toggleExpand(provider.id)}>
								+{provider.models.length - 4} {$t('common.models')}
							</button>
						{/if}
					</div>
				{:else}
					<div class="no-models">{$t('providers.fetchFirst')}</div>
				{/if}
			</article>
		{/each}
	</div>
	{/if}
{/if}

{#if showModal}
	<Modal title={editing ? $t('providers.edit') : $t('providers.add')} onClose={() => (showModal = false)}>
		{#if !editing}
			<div class="preset-row">
				{#each presets as preset}
					<button class="btn btn-sm" onclick={() => applyPreset(preset)}>{preset.name}</button>
				{/each}
			</div>
		{/if}
		<div class="form-grid">
			<div class="field">
				<label for="provider-name">{$t('common.name')}<span class="required-mark" aria-hidden="true">*</span></label>
				<input id="provider-name" class="input" bind:value={form.name} placeholder="DeepSeek" />
			</div>
			<div class="field">
				<label for="provider-id">{$t('providers.id')}<span class="required-mark" aria-hidden="true">*</span></label>
				<input id="provider-id" class="input mono" bind:value={form.id} disabled={editing} placeholder="ds" />
				<span class="hint">{$t('providers.idHint')}</span>
			</div>
			<div class="field full">
				<label for="provider-base">{$t('providers.baseUrl')}<span class="required-mark" aria-hidden="true">*</span></label>
				<div class="input-group">
					<input id="provider-base" class="input mono" bind:value={form.base_url} placeholder="https://api.deepseek.com/v1" />
					<select
						id="provider-api-type"
						class="select api-type-select"
						bind:value={form.api_type}
						title={$t('providers.apiType')}
						aria-label={$t('providers.apiType')}
					>
						<option value="completions">{$t('providers.apiTypeCompletionsShort')}</option>
						<option value="responses">{$t('providers.apiTypeResponsesShort')}</option>
						<option value="anthropic">{$t('providers.apiTypeAnthropicShort')}</option>
					</select>
				</div>
				<span class="hint endpoint-hint">
					{$t('providers.apiEndpoint')}
					<code class="mono">{apiEndpoint || '—'}</code>
				</span>
			</div>
			<div class="field full">
				<label for="provider-key">{$t('providers.apiKey')}</label>
				<div class="input-group">
					<input
						id="provider-key"
						class="input mono"
						value={keyFocused ? form.api_key : maskKey(form.api_key)}
						placeholder="sk-..."
						autocomplete="off"
						onfocus={() => (keyFocused = true)}
						onblur={() => (keyFocused = false)}
						oninput={(event) => (form.api_key = event.currentTarget.value)}
					/>
					<CopyButton value={form.api_key} size={14} buttonClass="btn input-group-btn" />
				</div>
				<span class="hint">{$t('providers.apiKeyHint')}</span>
			</div>
			<div class="field full">
				<div class="field-title-row">
					<label for="provider-proxy" class="field-label">{$t('providers.proxy')}</label>
					<span class="optional-label">{$t('common.optional')}</span>
				</div>
				<div class="input-group">
					<input
						id="provider-proxy"
						class="input mono"
						bind:value={form.proxy}
						oninput={() => (proxyResult = null)}
					/>
					<button class="btn input-group-btn" onclick={testProxy} disabled={proxyTesting || !form.proxy || !form.base_url}>
						{#if proxyTesting}<span class="spinner"></span>{:else}<Icon name="route" size={14} />{/if}
						{proxyTesting ? $t('providers.testingProxy') : $t('providers.testProxy')}
					</button>
				</div>
				{#if proxyResult}
					<span class="proxy-result" class:bad={!proxyResult.ok}>
						{#if proxyResult.ok}
							<Icon name="check" size={13} />
							{$t('providers.proxyOk', { ms: proxyResult.latency_ms, status: proxyResult.status_code })}
						{:else}
							<Icon name="alert" size={13} />
							{proxyResult.error}
						{/if}
					</span>
				{/if}
			</div>
			<div class="field full">
				<div class="field-title-row model-title-row">
					<div class="row model-head-left">
						<span class="field-label">{$t('providers.models')}</span>
						<span class="badge">{selectedModels.length}</span>
						<button class="btn btn-sm" onclick={syncModels} disabled={syncing || !form.base_url}>
							{#if syncing}<span class="spinner"></span>{:else}<Icon name="refresh" size={13} />{/if}
							{syncing ? $t('providers.syncing') : $t('providers.syncModels')}
						</button>
						<button class="btn btn-sm" onclick={testConnection} disabled={testing || !form.base_url}>
							{#if testing}<span class="spinner"></span>{:else}<Icon name="zap" size={13} />{/if}
							{$t('providers.testConnection')}
						</button>
					</div>
					<div class="model-tabs" role="tablist">
						<button class="model-tab" class:active={modelTab === 'list'} onclick={() => selectTab('list')} role="tab" aria-selected={modelTab === 'list'}>
							{$t('providers.modelTabList')}
						</button>
						<button class="model-tab" class:active={modelTab === 'selected'} onclick={() => selectTab('selected')} role="tab" aria-selected={modelTab === 'selected'}>
							{$t('providers.modelTabSelected')}
						</button>
						<button class="model-tab" class:active={modelTab === 'usage'} onclick={() => selectTab('usage')} role="tab" aria-selected={modelTab === 'usage'}>
							{$t('providers.modelTabUsage')}
						</button>
					</div>
				</div>
				<div class="model-chips">
					{#each modelCatalog as model (model.id)}
						<button
							class="model-pick"
							class:active={selectedIDs.includes(model.id)}
							onclick={() => toggleModel(model)}
							title={model.id}
							aria-pressed={selectedIDs.includes(model.id)}
						>
							<span class="model-pick-name">{model.name || model.id}</span>
							{#if model.last_test}
								<span class="model-size">{model.last_test.latency_ms} ms</span>
							{/if}
							<span class="model-dot {modelStatus(model)}"></span>
						</button>
					{:else}
						<span class="hint">{$t('providers.modelsNoCandidates')}</span>
					{/each}
				</div>
				<span class="hint">{$t('providers.modelsHint')}</span>
			</div>
			<div class="form-actions full">
				<div class="row">
					<button class="switch" class:on={form.enabled} onclick={() => (form.enabled = !form.enabled)} aria-label={$t('common.enabled')}></button>
					<span class="hint">{$t('providers.enabledHint')}</span>
				</div>
			</div>
		</div>
		<div slot="footer" class="modal-foot">
			<button class="btn" onclick={() => (showModal = false)}>{$t('common.cancel')}</button>
			<button class="btn btn-primary" onclick={save} disabled={saving || !form.name || !form.id || !form.base_url}>
				{#if saving}<span class="spinner"></span>{/if}
				{$t('common.save')}
			</button>
		</div>
	</Modal>
{/if}

<style>
	.view-toggle {
		display: flex;
		gap: 4px;
		margin-right: auto;
	}
	.view-toggle .btn.active {
		background: var(--accent-soft);
		color: var(--accent);
		border-color: rgba(52, 211, 153, 0.22);
	}
	.provider-list-card {
		padding: 0;
		overflow-x: auto;
	}
	.provider-table {
		width: 100%;
		border-collapse: collapse;
		font-size: 12px;
	}
	.provider-table th {
		padding: 12px 14px;
		border-bottom: 1px solid var(--line-soft);
		font-size: 10px;
		font-weight: 650;
		letter-spacing: 0.04em;
		text-transform: uppercase;
		color: var(--muted);
		text-align: left;
		white-space: nowrap;
	}
	.provider-table td {
		padding: 11px 14px;
		border-bottom: 1px solid var(--line-soft);
		vertical-align: middle;
	}
	.provider-table tr:last-child td {
		border-bottom: 0;
	}
	.provider-table tr.disabled {
		opacity: 0.6;
	}
	.list-identity {
		display: flex;
		align-items: center;
		gap: 10px;
		min-width: 220px;
	}
	.provider-avatar.small {
		width: 30px;
		height: 30px;
		border-radius: 9px;
		font-size: 12px;
	}
	.list-names {
		min-width: 0;
	}
	.list-title {
		display: flex;
		align-items: center;
		gap: 6px;
		flex-wrap: wrap;
		font-weight: 650;
	}
	.list-sub {
		margin-top: 3px;
		font-size: 10px;
		color: var(--muted);
		overflow-wrap: anywhere;
	}
	.list-models {
		color: var(--text-soft);
		font-variant-numeric: tabular-nums;
	}
	.list-actions {
		justify-content: flex-end;
		gap: 2px;
	}
	.provider-grid {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(430px, 1fr));
		gap: 16px;
	}
	.provider-card {
		padding: 18px;
		display: flex;
		flex-direction: column;
		gap: 14px;
		transition: opacity 0.15s ease;
	}
	.provider-card.disabled {
		opacity: 0.72;
	}
	.provider-head {
		display: flex;
		align-items: flex-start;
		justify-content: space-between;
		gap: 12px;
	}
	.provider-identity {
		display: flex;
		gap: 11px;
		min-width: 0;
	}
	.provider-avatar {
		width: 36px;
		height: 36px;
		border-radius: 10px;
		display: grid;
		place-items: center;
		background: linear-gradient(135deg, rgba(52, 211, 153, 0.17), rgba(34, 211, 238, 0.12));
		border: 1px solid rgba(52, 211, 153, 0.16);
		color: var(--accent);
		font-weight: 750;
		font-size: 15px;
		flex: none;
	}
	.provider-title-row {
		display: flex;
		align-items: center;
		gap: 8px;
		flex-wrap: wrap;
	}
	.provider-title-row h2 {
		font-size: 15px;
		font-weight: 680;
		letter-spacing: -0.2px;
	}
	.provider-id {
		font-size: 10.5px;
		color: var(--muted);
		margin-top: 2px;
	}
	.provider-details {
		display: flex;
		flex-direction: column;
		gap: 9px;
		padding: 11px 12px;
		border: 1px solid var(--line-soft);
		border-radius: 9px;
		background: var(--surface-tint);
	}
	.detail {
		display: flex;
		flex-direction: column;
		gap: 3px;
		min-width: 0;
	}
	.detail-label {
		font-size: 9.5px;
		text-transform: uppercase;
		letter-spacing: 0.07em;
		color: var(--muted);
		font-weight: 700;
	}
	.detail-value {
		font-size: 11px;
		color: var(--text-soft);
		overflow-wrap: anywhere;
	}
	.key-row {
		display: flex;
		align-items: center;
		gap: 4px;
		min-width: 0;
	}
	.key-value {
		font-size: 10.5px;
		color: var(--key-text);
		background: var(--key-bg);
		border: 1px solid var(--key-border);
		border-radius: 6px;
		padding: 4px 7px;
		overflow-wrap: anywhere;
		flex: 1;
	}
	.models-head {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 10px;
	}
	.model-list {
		display: flex;
		flex-direction: column;
		border: 1px solid var(--line-soft);
		border-radius: 9px;
		overflow: hidden;
	}
	.model-row {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 10px;
		padding: 7px 9px;
		border-bottom: 1px solid var(--line-soft);
	}
	.model-row:last-child {
		border-bottom: 0;
	}
	.model-name {
		display: flex;
		align-items: center;
		gap: 7px;
		min-width: 0;
		flex-wrap: wrap;
	}
	.model-id {
		font-size: 10.5px;
		color: var(--text-soft);
		overflow-wrap: anywhere;
	}
	.model-label {
		font-size: 10px;
		color: var(--muted);
	}
	.model-test-error {
		width: 100%;
		font-size: 10px;
		line-height: 1.4;
		color: var(--danger-text);
		overflow-wrap: anywhere;
	}
	.more-models {
		border: 0;
		background: var(--badge-bg);
		color: var(--muted);
		font-size: 10.5px;
		padding: 8px;
	}
	.more-models:hover {
		color: var(--accent);
		background: var(--accent-soft);
	}
	.no-models {
		font-size: 11px;
		color: var(--muted);
		padding: 9px 11px;
		border: 1px dashed var(--line);
		border-radius: 8px;
	}
	.preset-row {
		display: flex;
		gap: 7px;
		flex-wrap: wrap;
	}
	.field-title-row {
		display: flex;
		align-items: center;
		gap: 7px;
	}
	.model-title-row {
		justify-content: space-between;
		flex-wrap: wrap;
	}
	.optional-label {
		font-size: 10px;
		color: var(--muted);
		font-weight: 500;
		text-transform: none;
		letter-spacing: 0;
	}
	.endpoint-hint {
		display: flex;
		align-items: baseline;
		gap: 5px;
		flex-wrap: wrap;
	}
	.endpoint-hint code {
		color: var(--text-soft);
		background: var(--surface-code);
		border: 1px solid var(--line-soft);
		border-radius: 4px;
		padding: 1px 5px;
		font-size: 10px;
		overflow-wrap: anywhere;
	}
	.input-group {
		display: flex;
		align-items: stretch;
		width: 100%;
	}
	.input-group > .input {
		flex: 1;
		min-width: 0;
		border-top-right-radius: 0;
		border-bottom-right-radius: 0;
		border-right: 0;
	}
	.input-group > .select {
		flex: none;
		width: auto;
		min-width: 124px;
		border-top-left-radius: 0;
		border-bottom-left-radius: 0;
	}
	.input-group .input:focus {
		position: relative;
		z-index: 1;
	}
	.input-group-btn {
		border-top-left-radius: 0;
		border-bottom-left-radius: 0;
		flex: none;
	}
	.model-head-left {
		gap: 7px;
		flex-wrap: wrap;
	}
	.model-tabs {
		display: inline-flex;
		gap: 2px;
		padding: 2px;
		border: 1px solid var(--line-soft);
		border-radius: 8px;
		background: var(--surface-inset);
	}
	.model-tab {
		border: 0;
		background: transparent;
		color: var(--muted);
		font-size: 11px;
		padding: 4px 10px;
		border-radius: 6px;
		cursor: pointer;
		white-space: nowrap;
	}
	.model-tab.active {
		background: var(--panel);
		color: var(--text);
		font-weight: 650;
	}
	.model-chips {
		display: flex;
		flex-wrap: wrap;
		gap: 8px;
		min-height: 38px;
		padding: 10px;
		border: 1px solid var(--line-soft);
		border-radius: 9px;
		background: var(--surface-inset);
	}
	.model-chips > .hint {
		padding: 4px 2px;
	}
	.model-pick {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		max-width: 100%;
		padding: 5px 10px;
		border: 1px dashed var(--line);
		border-radius: 999px;
		background: var(--panel);
		color: var(--text-soft);
		font-size: 11px;
		cursor: pointer;
	}
	.model-pick:hover {
		border-color: var(--accent);
	}
	.model-pick.active {
		border-color: var(--accent);
		background: var(--accent-soft);
		color: var(--accent);
	}
	.model-pick-name {
		overflow-wrap: anywhere;
	}
	.model-size {
		padding: 1px 5px;
		border-radius: 4px;
		background: var(--surface-inset);
		color: var(--muted);
		font-size: 9.5px;
		white-space: nowrap;
	}
	.model-dot {
		width: 6px;
		height: 6px;
		border-radius: 50%;
		background: var(--line);
		flex: none;
	}
	.model-dot.ok,
	.model-dot.selected {
		background: var(--accent);
	}
	.model-dot.bad {
		background: var(--danger);
	}
	@media (max-width: 720px) {
	.provider-grid {
			grid-template-columns: 1fr;
		}
	}
</style>
