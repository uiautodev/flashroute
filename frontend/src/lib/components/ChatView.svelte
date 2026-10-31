<script>
	import { onMount, tick } from 'svelte';
	import { api, relativeTime } from '#lib/api.js';
	import { t } from '#lib/i18n.js';
	import { toast } from '#lib/toast.js';
	import Icon from './Icon.svelte';
	import CopyButton from './CopyButton.svelte';
	import { marked } from 'marked';
	import DOMPurify from 'dompurify';

	marked.setOptions({ gfm: true, breaks: true });

	export let onNavigate = () => {};

	let loading = true;
	let available = { routes: [], direct: [] };
	let chats = [];
	let current = null;
	let messages = [];
	let model = '';
	let privacy = false;
	let webSearch = typeof localStorage !== 'undefined' ? localStorage.getItem('flashroute.webSearch') !== 'false' : true;
	let text = '';
	let attachments = [];
	let sending = false;
	let editingID = '';
	let editingText = '';
	let savingEdit = false;
	let switchingVersion = false;
	let fullscreen = false;
	let historyOpen = false;
	let activeRun = null;
	let runSeq = 0;
	let composing = false;
	let compositionEndedAt = 0;
	let toolStatus = '';
	let streamingMessageID = '';
	let streamFrame = 0;
	let editBox;
	let error = '';
	let fileInput;
	let scroller;
	let textarea;
	let dragActive = false;

	$: routes = available.routes || [];
	$: direct = available.direct || [];
	$: hasModels = routes.length > 0 || direct.length > 0;
	$: canSend = model && (text.trim() || attachments.length > 0) && !editingID;

	onMount(() => () => {
		document.documentElement.classList.remove('chat-fullscreen');
		document.documentElement.classList.remove('chat-history-open');
	});

	onMount(async () => {
		model = localStorage.getItem('flashroute.model') || '';
		await load();
	});

	async function load() {
		loading = true;
		try {
			const [models, list] = await Promise.all([api.get('/api/models/available'), api.get('/api/chats')]);
			available = models;
			chats = list.chats || [];
			const routeList = models.routes || [];
			const directList = models.direct || [];
			const exists = (id) => routeList.some((r) => r.alias === id) || directList.some((d) => d.id === id);
			if (!model && routeList.length) model = routeList[0].alias;
			if (!model && directList.length) model = directList[0].id;
			if (model && !exists(model)) {
				model = routeList[0]?.alias || directList[0]?.id || '';
			}
		} catch (err) {
			error = err.message;
		} finally {
			loading = false;
		}
	}

	function modelExists(id) {
		return routes.some((r) => r.alias === id) || direct.some((d) => d.id === id);
	}

	function chooseModel() {
		localStorage.setItem('flashroute.model', model);
	}

	async function newChat() {
		closeHistory();
		current = null;
		messages = [];
		error = '';
		text = '';
		attachments = [];
		editingID = '';
		editingText = '';
		await tick();
		textarea?.focus();
	}

	async function openChat(chat) {
		closeHistory();
		try {
			const data = await api.get('/api/chats/' + chat.id);
			current = data;
			messages = data.messages || [];
			editingID = '';
			editingText = '';
			if (data.model && modelExists(data.model)) model = data.model;
			await refreshChats();
			await scrollBottom();
		} catch (err) {
			toast(err.message, 'error');
		}
	}

	async function clearHistory() {
		if (!chats.length || !confirm($t('chat.clearConfirm'))) return;
		try {
			await api.del('/api/chats');
			chats = [];
			await newChat();
			toast($t('chat.cleared'), 'success');
		} catch (err) {
			toast(err.message, 'error');
		}
	}

	async function removeChat(chat, event) {
		event?.stopPropagation();
		if (!confirm($t('chat.deleteConfirm'))) return;
		try {
			await api.del('/api/chats/' + chat.id);
			chats = chats.filter((c) => c.id !== chat.id);
			if (current?.id === chat.id) await newChat();
			toast($t('common.delete'), 'success');
		} catch (err) {
			toast(err.message, 'error');
		}
	}

	function startRun(snapshot, input, images) {
		const id = ++runSeq;
		const controller = new AbortController();
		const run = { id, controller, snapshot, input, images };
		activeRun = run;
		return run;
	}

	function interrupt({ restoreInput = false } = {}) {
		const run = activeRun;
		if (!run) return;
		run.controller.abort();
		if (run.id !== runSeq) return;
		runSeq++;
		activeRun = null;
		messages = run.snapshot;
		if (restoreInput && run.input !== null && !text.trim() && attachments.length === 0) {
			text = run.input;
			attachments = run.images.map((url, index) => ({ name: `image-${index + 1}`, dataUrl: url }));
		}
		sending = false;
		savingEdit = false;
		editingID = '';
		streamingMessageID = '';
		error = '';
		scrollBottom();
	}

	function stopGeneration() {
		interrupt({ restoreInput: true });
	}

	async function streamChat(payload, assistantID, signal) {
		try {
			await api.stream(
				'/api/chat',
				payload,
				(event, data) => handleStreamEvent(assistantID, event, data),
				{ signal }
			);
		} catch (err) {
			if (!/streaming is not supported/i.test(err.message || '')) throw err;
			const data = await api.post('/api/chat', { ...payload, stream: false }, { signal });
			messages = data.chat.messages || [];
			if (!privacy) current = data.chat;
		}
	}

	async function send() {
		error = '';
		if (!model) {
			error = $t('chat.errorNoModel');
			return;
		}
		if (!text.trim() && attachments.length === 0) {
			error = $t('chat.errorNoMessage');
			return;
		}
		if (activeRun) interrupt({ restoreInput: false });
		const outgoingText = text.trim();
		const outgoingImages = attachments.map((item) => item.dataUrl);
		const previousMessages = messages;
		const run = startRun(previousMessages, outgoingText, outgoingImages);
		const history = messages
			.filter((m) => m.role === 'user' || m.role === 'assistant')
			.map((m) => ({ id: m.id, role: m.role, content: m.content, images: m.images || [], created_at: m.created_at }));
		const assistantID = 'stream-' + Date.now();
		const optimistic = {
			id: 'local-' + Date.now(),
			role: 'user',
			content: outgoingText,
			images: outgoingImages,
			created_at: new Date().toISOString()
		};
		const placeholder = {
			id: assistantID,
			role: 'assistant',
			content: '',
			reasoning: '',
			created_at: new Date().toISOString()
		};
		messages = [...messages, optimistic, placeholder];
		streamingMessageID = assistantID;
		toolStatus = '';
		text = '';
		attachments = [];
		sending = true;
		await scrollBottom();
		try {
			await streamChat(
				{
					chat_id: privacy ? '' : current?.id || '',
					model,
					message: outgoingText,
					images: outgoingImages,
					privacy,
					web_search: webSearch,
					stream: true,
					history
				},
				assistantID,
				run.controller.signal
			);
			if (!privacy) await refreshChats();
			await scrollBottom();
		} catch (err) {
			if (run.id !== runSeq || run.controller.signal.aborted) return;
			messages = previousMessages;
			text = outgoingText;
			attachments = outgoingImages.map((url, index) => ({ name: `image-${index + 1}`, dataUrl: url }));
			error = err.message;
			toast(err.message, 'error');
		} finally {
			if (run.id === runSeq) {
				activeRun = null;
				sending = false;
				streamingMessageID = '';
				await scrollBottom();
				textarea?.focus();
			}
		}
	}

	async function startEdit(message) {
		if (message.role !== 'user' || sending || savingEdit) return;
		editingID = message.id;
		editingText = message.content;
		await tick();
		editBox?.focus();
	}

	function cancelEdit() {
		editingID = '';
		editingText = '';
	}

	async function saveEdit(message, regenerate = false) {
		if ((!regenerate && !editingText.trim()) || savingEdit) return;
		const nextText = regenerate ? message.content : editingText.trim();
		const index = messages.findIndex((item) => item.id === message.id);
		if (index < 0) return;
		if (activeRun) interrupt({ restoreInput: false });
		const previousMessages = messages;
		const run = startRun(previousMessages, null, []);
		const history = privacy
			? messages.map((m) => ({ id: m.id, role: m.role, content: m.content, images: m.images || [], created_at: m.created_at }))
			: [];
		const assistantID = 'stream-' + Date.now();
		const editedUser = { ...messages[index], content: nextText };
		messages = [
			...messages.slice(0, index),
			editedUser,
			{ id: assistantID, role: 'assistant', content: '', reasoning: '', created_at: new Date().toISOString() }
		];
		if (!regenerate) editingID = '';
		streamingMessageID = assistantID;
		toolStatus = '';
		savingEdit = true;
		error = '';
		await scrollBottom();
		try {
			await streamChat(
				{
					chat_id: privacy ? '' : current?.id || '',
					model,
					message: nextText,
					images: [],
					privacy,
					web_search: webSearch,
					stream: true,
					edit_message_id: message.id,
					history
				},
				assistantID,
				run.controller.signal
			);
			editingText = '';
			if (!privacy) await refreshChats();
			await scrollBottom();
		} catch (err) {
			if (run.id !== runSeq || run.controller.signal.aborted) return;
			messages = previousMessages;
			if (!regenerate) {
				editingID = message.id;
				await tick();
				editBox?.focus();
			}
			error = err.message;
			toast(err.message, 'error');
		} finally {
			if (run.id === runSeq) {
				activeRun = null;
				savingEdit = false;
				streamingMessageID = '';
			}
		}
	}

	async function regenerate(message) {
		if (!current?.id || privacy || sending || savingEdit) return;
		const index = messages.findIndex((item) => item.id === message.id);
		if (index < 0) return;
		if (activeRun) interrupt({ restoreInput: false });
		const previousMessages = messages;
		const run = startRun(previousMessages, null, []);
		const assistantID = 'stream-' + Date.now();
		messages = messages.map((item, i) =>
			i === index ? { ...item, id: assistantID, content: '', reasoning: '' } : item
		);
		streamingMessageID = assistantID;
		savingEdit = true;
		toolStatus = '';
		error = '';
		await scrollBottom();
		try {
			await streamChat(
				{
					chat_id: current.id,
					model,
					web_search: webSearch,
					regenerate_message_id: message.id,
					privacy: false,
					stream: true,
					history: []
				},
				assistantID,
				run.controller.signal
			);
			await refreshChats();
			await scrollBottom();
		} catch (err) {
			if (run.id !== runSeq || run.controller.signal.aborted) return;
			messages = previousMessages;
			error = err.message;
			toast(err.message, 'error');
		} finally {
			if (run.id === runSeq) {
				activeRun = null;
				savingEdit = false;
				streamingMessageID = '';
			}
		}
	}

	async function switchVersion(message, direction) {
		if (!current?.id || switchingVersion) return;
		const index = (message.version_index || 1) + direction;
		if (index < 1 || index > (message.version_count || 1)) return;
		switchingVersion = true;
		error = '';
		try {
			const data = await api.post(
				`/api/chats/${current.id}/messages/${encodeURIComponent(message.id)}/version`,
				{ index }
			);
			current = data;
			messages = data.messages || [];
			editingID = '';
			editingText = '';
			if (data.model && modelExists(data.model)) model = data.model;
			await refreshChats();
			await scrollBottom();
		} catch (err) {
			error = err.message;
			toast(err.message, 'error');
		} finally {
			switchingVersion = false;
		}
	}

	function handleStreamEvent(assistantID, event, data) {
		if (event === 'reasoning') {
			const delta = data.content || '';
			if (delta) {
				messages = messages.map((message) =>
					message.id === assistantID ? { ...message, reasoning: (message.reasoning || '') + delta } : message
				);
				scheduleScroll();
			}
		} else if (event === 'delta') {
			const delta = data.content || '';
			if (delta) {
				messages = messages.map((message) =>
					message.id === assistantID ? { ...message, content: message.content + delta } : message
				);
				scheduleScroll();
			}
		} else if (event === 'tool') {
			toolStatus = data.name === 'web_fetch' ? $t('chat.fetchingPage') : $t('chat.searchingWeb');
		} else if (event === 'tool_done') {
			toolStatus = '';
		} else if (event === 'done') {
			toolStatus = '';
			messages = data.chat.messages || [];
			if (!privacy) current = data.chat;
		}
	}

	function renderMarkdown(content) {
		if (!content) return '';
		return DOMPurify.sanitize(marked.parse(content), { USE_PROFILES: { html: true } });
	}

	async function refreshChats() {
		try {
			const list = await api.get('/api/chats');
			chats = list.chats || [];
		} catch {
			// Keep the current list if refreshing fails.
		}
	}

	function onCompositionStart() {
		composing = true;
	}

	function onCompositionEnd() {
		composing = false;
		compositionEndedAt = performance.now();
	}

	function isImeEnter(event) {
		return composing || event.isComposing || event.keyCode === 229 || performance.now() - compositionEndedAt < 80;
	}

	function toolLabel(tools) {
		const names = tools.map((tool) => (tool === 'web_fetch' ? $t('chat.webFetchTool') : $t('chat.webSearchTool')));
		return $t('chat.usedTools', { tools: names.join(' · ') });
	}

	function reasoningOpen(message) {
		if (typeof message.reasoningOpen === 'boolean') return message.reasoningOpen;
		return !message.content;
	}

	function toggleReasoning(message) {
		messages = messages.map((item) =>
			item.id === message.id ? { ...item, reasoningOpen: !reasoningOpen(message) } : item
		);
	}

	function onKeydown(event) {
		if (event.key !== 'Enter' || event.shiftKey) return;
		if (isImeEnter(event)) return;
		event.preventDefault();
		if (canSend) send();
	}

	function autoGrow() {
		if (!textarea) return;
		textarea.style.height = 'auto';
		textarea.style.height = Math.min(textarea.scrollHeight, 180) + 'px';
	}

	async function addFiles(files) {
		const images = [...files].filter((file) => file.type.startsWith('image/'));
		if (attachments.length + images.length > 4) {
			toast($t('chat.imageLimit'), 'error');
			return;
		}
		for (const file of images) {
			if (file.size > 10 * 1024 * 1024) {
				toast($t('chat.imageTooLarge'), 'error');
				continue;
			}
			const dataUrl = await fileToDataURL(file);
			attachments = [...attachments, { name: file.name, dataUrl }];
		}
		if (fileInput) fileInput.value = '';
	}

	function fileToDataURL(file) {
		return new Promise((resolve, reject) => {
			const reader = new FileReader();
			reader.onload = () => resolve(reader.result);
			reader.onerror = reject;
			reader.readAsDataURL(file);
		});
	}

	function onPaste(event) {
		const files = event.clipboardData?.files;
		if (files?.length) {
			event.preventDefault();
			addFiles(files);
		}
	}

	function onDrop(event) {
		event.preventDefault();
		dragActive = false;
		if (event.dataTransfer?.files?.length) addFiles(event.dataTransfer.files);
	}

	async function scrollBottom() {
		await tick();
		if (scroller) scroller.scrollTop = scroller.scrollHeight;
	}

	function scheduleScroll() {
		if (streamFrame) return;
		streamFrame = requestAnimationFrame(() => {
			streamFrame = 0;
			if (scroller) scroller.scrollTop = scroller.scrollHeight;
		});
	}

	function removeAttachment(index) {
		attachments = attachments.filter((_, i) => i !== index);
	}

	function togglePrivacy() {
		privacy = !privacy;
		if (privacy) current = null;
	}

	function toggleWebSearch() {
		webSearch = !webSearch;
		localStorage.setItem('flashroute.webSearch', String(webSearch));
	}

	function closeHistory() {
		historyOpen = false;
		document.documentElement.classList.remove('chat-history-open');
	}

	function toggleHistory() {
		historyOpen = !historyOpen;
		document.documentElement.classList.toggle('chat-history-open', fullscreen && historyOpen);
	}

	function toggleFullscreen() {
		fullscreen = !fullscreen;
		document.documentElement.classList.toggle('chat-fullscreen', fullscreen);
		if (!fullscreen) closeHistory();
	}
</script>

<div
	class="chat-layout"
	class:drag-active={dragActive}
	role="presentation"
	ondragover={(e) => {
		e.preventDefault();
		dragActive = true;
	}}
	ondragleave={() => (dragActive = false)}
	ondrop={onDrop}
>
	{#if fullscreen}
		<div class="chat-history-scrim" class:show={historyOpen} role="presentation" onclick={closeHistory}></div>
	{/if}
	<aside class="chat-history card">
		<div class="history-head">
			<div>
				<div class="history-eyebrow">{$t('chat.history')}</div>
				<div class="history-count">{chats.length}</div>
			</div>
			<div class="history-head-actions">
				<button
					class="btn btn-ghost btn-icon"
					onclick={clearHistory}
					disabled={!chats.length || loading}
					title={$t('chat.clearHistory')}
					aria-label={$t('chat.clearHistory')}
				>
					<Icon name="trash" size={14} />
				</button>
				<button class="btn btn-sm btn-primary" onclick={newChat} title={$t('chat.newChat')}>
					<Icon name="plus" size={14} />
					{$t('chat.newChat')}
				</button>
			</div>
		</div>
		{#if privacy}
			<div class="private-chip">
				<Icon name="shield" size={13} />
				{$t('chat.privateNotSaved')}
			</div>
		{/if}
		<div class="history-list">
			{#if loading}
				<div class="history-loading">
					<div class="skeleton" style="width:80%"></div>
					<div class="skeleton" style="width:60%"></div>
					<div class="skeleton" style="width:70%"></div>
				</div>
			{:else if chats.length === 0}
				<div class="history-empty">{$t('chat.noChats')}</div>
			{:else}
				{#each chats as chat (chat.id)}
					<div
						class="history-item"
						class:active={current?.id === chat.id}
						role="button"
						tabindex="0"
						onclick={() => openChat(chat)}
						onkeydown={(event) => {
							if (event.key === 'Enter' || event.key === ' ') openChat(chat);
						}}
					>
						<div class="history-main">
							<span class="history-title">{chat.title || $t('chat.newChat')}</span>
							<span class="history-meta">{chat.model} · {relativeTime(chat.updated_at)}</span>
							{#if chat.preview}<span class="history-preview">{chat.preview}</span>{/if}
						</div>
						<button class="history-delete" onclick={(event) => removeChat(chat, event)} title={$t('chat.delete')}>
							<Icon name="trash" size={13} />
						</button>
					</div>
				{/each}
			{/if}
		</div>
	</aside>

	<section class="chat-panel card">
		<header class="chat-head">
			<div class="chat-head-main">
				<div class="model-select-wrap">
					<div class="chat-head-label-row">
						<span class="chat-head-label">{$t('chat.model')}</span>
						{#if current?.title}<span class="current-title" title={current.title}>{current.title}</span>{/if}
					</div>
					<select class="select model-select" bind:value={model} onchange={chooseModel} disabled={!hasModels}>
						{#if !hasModels}
							<option value="">{$t('chat.noModelsTitle')}</option>
						{/if}
						{#if routes.length}
							<optgroup label={$t('chat.routes')}>
								{#each routes as route (route.alias)}
									<option value={route.alias}>{route.alias} · {route.targets.length} {$t('common.models')}</option>
								{/each}
							</optgroup>
						{/if}
						{#if direct.length}
							<optgroup label={$t('chat.direct')}>
								{#each direct as item (item.id)}
									<option value={item.id}>{item.id}</option>
								{/each}
							</optgroup>
						{/if}
					</select>
				</div>
			</div>
			<div class="chat-head-right">
				<div class="websearch-control">
					<Icon name="globe" size={15} />
					<span>{$t('chat.webSearch')}</span>
					<button
						class="switch small"
						class:on={webSearch}
						onclick={toggleWebSearch}
						aria-label={$t('chat.webSearch')}
						aria-pressed={webSearch}
						title={$t('chat.webSearchHint')}
					></button>
				</div>
				<div class="privacy-control">
					<Icon name="shield" size={15} />
					<span>{$t('chat.privacy')}</span>
					<button class="switch small" class:on={privacy} onclick={togglePrivacy} aria-label={$t('chat.privacy')}></button>
					<span class="privacy-state">{privacy ? $t('chat.privacyOn') : $t('chat.privacyOff')}</span>
				</div>
				{#if fullscreen}
					<button
						class="btn btn-ghost btn-icon"
						onclick={toggleHistory}
						title={$t('chat.history')}
						aria-label={$t('chat.history')}
						aria-expanded={historyOpen}
					>
						<Icon name="clock" size={16} />
					</button>
					<button class="btn btn-ghost btn-icon" onclick={newChat} title={$t('chat.newChat')} aria-label={$t('chat.newChat')}>
						<Icon name="plus" size={16} />
					</button>
				{/if}
				<button
					class="btn btn-ghost btn-icon"
					onclick={toggleFullscreen}
					title={fullscreen ? $t('chat.exitFullscreen') : $t('chat.fullscreen')}
					aria-label={fullscreen ? $t('chat.exitFullscreen') : $t('chat.fullscreen')}
				>
					<Icon name={fullscreen ? 'minimize' : 'maximize'} size={16} />
				</button>
			</div>
		</header>

		{#if !hasModels && !loading}
			<div class="chat-empty">
				<div class="empty-icon"><Icon name="server" size={22} /></div>
				<h2 class="empty-title">{$t('chat.noModelsTitle')}</h2>
				<p class="empty-text">{$t('chat.noModelsBody')}</p>
				<button class="btn btn-primary" onclick={() => onNavigate('providers')}>{$t('chat.goProviders')}</button>
			</div>
		{:else}
			<div class="messages" bind:this={scroller}>
				{#if messages.length === 0}
					<div class="welcome">
						<div class="welcome-mark"><Icon name="zap" size={30} /></div>
						<h2>{$t('chat.emptyTitle')}</h2>
						<p>{$t('chat.emptyBody')}</p>
						{#if privacy}
							<span class="badge badge-warn"><Icon name="shield" size={12} />{$t('chat.privateNotSaved')}</span>
						{/if}
					</div>
				{/if}
				{#each messages as message (message.id)}
					<div class="message {message.role}">
						{#if message.role !== 'user'}
							<div class="avatar"><Icon name="zap" size={14} /></div>
						{/if}
						<div class="message-body">
							{#if message.role !== 'user'}
							<div class="message-meta">
								<span>FlashRoute</span>
								{#if message.model}<span class="mono">{message.model}</span>{/if}
								{#if message.provider}<span>{message.provider}</span>{/if}
							</div>
							{/if}
							{#if message.images?.length}
								<div class="message-images">
									{#each message.images as image}
										<img src={image} alt="attachment" />
									{/each}
								</div>
							{/if}
							{#if editingID === message.id}
								<div class="message-editor">
									<textarea
										bind:this={editBox}
										class="edit-input"
										bind:value={editingText}
										onkeydown={(event) => {
											if (event.key === 'Escape') cancelEdit();
											if (event.key === 'Enter' && (event.metaKey || event.ctrlKey) && !isImeEnter(event)) {
												event.preventDefault();
												saveEdit(message);
											}
										}}
										rows="4"
									></textarea>
									<div class="edit-hint">{$t('chat.editHint')}</div>
									<div class="edit-actions">
										<button class="btn btn-sm" onclick={cancelEdit}>{$t('common.cancel')}</button>
										<button class="btn btn-primary btn-sm" onclick={() => saveEdit(message)} disabled={savingEdit || !editingText.trim()}>
											{#if savingEdit}<span class="spinner"></span>{/if}
											{$t('chat.saveEdit')}
										</button>
									</div>
								</div>
							{:else if message.id === streamingMessageID && !message.content && !message.reasoning}
								<div class="thinking"><span class="spinner"></span>{toolStatus || $t('chat.sending')}</div>
							{:else}
								{#if message.reasoning && reasoningOpen(message)}
									<div class="reasoning-block">
										<div class="reasoning-text">{message.reasoning}</div>
									</div>
								{/if}
								{#if message.content}
									<div class="message-text markdown">
										{@html renderMarkdown(message.content)}
										{#if message.id === streamingMessageID}<span class="stream-cursor"></span>{/if}
									</div>
								{/if}
							{/if}
							{#if message.id !== streamingMessageID && editingID !== message.id}
								<div class="message-footer">
									<span class="message-actions">
										{#if message.role === 'user' && message.redacted}
											<span class="message-safe" title={$t('chat.redactedHint')} aria-label={$t('chat.redacted')}>
												<Icon name="shield" size={13} />
											</span>
										{/if}
										{#if message.role === 'user'}
											<button onclick={() => startEdit(message)} title={$t('chat.editMessage')} aria-label={$t('chat.editMessage')}>
												<Icon name="edit" size={13} />
											</button>
										{/if}
										{#if message.content}
											<CopyButton value={message.content} buttonClass="" label={$t('chat.copyMessage')} />
										{/if}
										{#if message.role === 'assistant' && !privacy}
											<button onclick={() => regenerate(message)} title={$t('chat.regenerate')} aria-label={$t('chat.regenerate')} disabled={sending || savingEdit}>
												<Icon name="refresh" size={13} />
											</button>
										{/if}
										{#if message.role === 'assistant' && message.tools?.length}
											<span class="tool-indicator" title={toolLabel(message.tools)} aria-label={toolLabel(message.tools)}>
												<Icon name="globe" size={13} />
											</span>
										{/if}
										{#if message.role === 'assistant' && message.reasoning}
											<button
												class:active={reasoningOpen(message)}
												onclick={() => toggleReasoning(message)}
												title={reasoningOpen(message) ? $t('chat.hideReasoning') : $t('chat.showReasoning')}
												aria-label={reasoningOpen(message) ? $t('chat.hideReasoning') : $t('chat.showReasoning')}
												aria-pressed={reasoningOpen(message)}
											>
												<Icon name="spark" size={13} />
											</button>
										{/if}
										{#if message.version_count > 1}
											<span class="revision-inline">
												<button
													onclick={() => switchVersion(message, -1)}
													disabled={switchingVersion || message.version_index <= 1}
													title={$t('chat.previousVersion')}
													aria-label={$t('chat.previousVersion')}
												>
													<Icon name="chevron-left" size={11} />
												</button>
												<span>{message.version_index}/{message.version_count}</span>
												<button
													onclick={() => switchVersion(message, 1)}
													disabled={switchingVersion || message.version_index >= message.version_count}
													title={$t('chat.nextVersion')}
													aria-label={$t('chat.nextVersion')}
												>
													<Icon name="chevron-right" size={11} />
												</button>
											</span>
										{/if}
									</span>
								</div>
							{/if}
						</div>
					</div>
				{/each}
			</div>

			{#if error}
				<div class="chat-error">
					<Icon name="alert" size={15} />
					<span>{error}</span>
					<button class="btn btn-ghost btn-icon" onclick={() => (error = '')}><Icon name="x" size={14} /></button>
				</div>
			{/if}

			<footer class="composer">
				{#if attachments.length}
					<div class="attachment-strip">
						{#each attachments as attachment, index}
							<div class="attachment">
								<img src={attachment.dataUrl} alt={attachment.name} />
								<button class="attachment-remove" onclick={() => removeAttachment(index)}>
									<Icon name="x" size={11} />
								</button>
							</div>
						{/each}
					</div>
				{/if}
				<div class="composer-box">
					<button class="btn btn-ghost btn-icon attach-btn" onclick={() => fileInput?.click()} title={$t('chat.attach')}>
						<Icon name="image" size={18} />
					</button>
					<input
						bind:this={fileInput}
						type="file"
						accept="image/png,image/jpeg,image/gif,image/webp"
						multiple
						hidden
						onchange={(e) => addFiles(e.currentTarget.files)}
					/>
					<textarea
						bind:this={textarea}
						class="chat-input"
						bind:value={text}
						oninput={autoGrow}
						onkeydown={onKeydown}
						oncompositionstart={onCompositionStart}
						oncompositionend={onCompositionEnd}
						onpaste={onPaste}
						placeholder={$t('chat.placeholder')}
						rows="1"
					></textarea>
					{#if activeRun}
						<button class="btn btn-danger btn-icon stop-btn" onclick={stopGeneration} title={$t('chat.stop')} aria-label={$t('chat.stop')}>
							<Icon name="stop" size={16} />
						</button>
					{/if}
					<button class="btn btn-primary send-btn" onclick={send} disabled={!canSend} title={$t('chat.send')}>
						<Icon name="send" size={16} />
					</button>
				</div>
				<div class="composer-hint">
					<span>{$t('chat.startHint')}</span>
					{#if privacy}
						<span class="privacy-note"><Icon name="shield" size={11} />{$t('chat.privacyHint')}</span>
					{/if}
				</div>
			</footer>
		{/if}
	</section>
</div>

<style>
	.chat-layout {
		display: grid;
		grid-template-columns: 266px minmax(0, 1fr);
		gap: 16px;
		height: calc(100vh - 140px);
		min-height: 500px;
	}
	.chat-layout.drag-active .chat-panel {
		border-color: var(--accent-border-strong);
		box-shadow: 0 0 0 3px var(--accent-ring);
	}
	.chat-history {
		display: flex;
		flex-direction: column;
		overflow: hidden;
		min-height: 0;
	}
	.history-head {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 8px;
		padding: 16px 14px 12px;
		border-bottom: 1px solid var(--line-soft);
	}
	.history-eyebrow {
		font-size: 10px;
		text-transform: uppercase;
		letter-spacing: 0.08em;
		color: var(--muted);
		font-weight: 700;
	}
	.history-count {
		font-size: 18px;
		font-weight: 700;
		margin-top: 2px;
	}
	.history-head-actions {
		display: flex;
		align-items: center;
		gap: 4px;
	}
	.private-chip {
		display: flex;
		align-items: center;
		gap: 6px;
		margin: 10px 12px 0;
		padding: 7px 9px;
		font-size: 10.5px;
		color: var(--warning);
		background: var(--warning-soft);
		border: 1px solid rgba(251, 191, 36, 0.18);
		border-radius: 8px;
	}
	.history-list {
		overflow-y: auto;
		padding: 8px;
		display: flex;
		flex-direction: column;
		gap: 4px;
		min-height: 0;
	}
	.history-loading {
		display: flex;
		flex-direction: column;
		gap: 12px;
		padding: 14px 10px;
	}
	.history-empty {
		padding: 24px 12px;
		text-align: center;
		color: var(--muted);
		font-size: 11.5px;
	}
	.history-item {
		position: relative;
		display: flex;
		gap: 6px;
		width: 100%;
		text-align: left;
		background: transparent;
		border: 1px solid transparent;
		border-radius: 9px;
		padding: 9px 9px 9px 10px;
		color: var(--text-soft);
		transition: background 0.14s ease;
	}
	.history-item:hover {
		background: var(--hover-tint);
	}
	.history-item.active {
		background: var(--accent-soft);
		border-color: var(--accent-border-soft);
	}
	.history-main {
		min-width: 0;
		flex: 1;
		display: flex;
		flex-direction: column;
		gap: 3px;
	}
	.history-title {
		font-size: 12px;
		font-weight: 600;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.history-meta,
	.history-preview {
		font-size: 10px;
		color: var(--muted);
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.history-preview {
		color: var(--muted);
	}
	.history-delete {
		opacity: 0;
		border: 0;
		background: transparent;
		color: var(--muted);
		padding: 4px;
		border-radius: 5px;
		height: fit-content;
		transition: opacity 0.12s ease;
	}
	.history-item:hover .history-delete {
		opacity: 1;
	}
	.history-delete:hover {
		background: var(--danger-soft);
		color: var(--danger);
	}
	.chat-history-scrim {
		display: none;
	}
	:global(html.chat-fullscreen .chat-history-scrim) {
		display: block;
		position: fixed;
		inset: 0;
		z-index: 30;
		background: var(--modal-backdrop);
		opacity: 0;
		pointer-events: none;
		transition: opacity 0.15s ease;
	}
	:global(html.chat-fullscreen .chat-history-scrim.show) {
		opacity: 1;
		pointer-events: auto;
	}
	:global(html.chat-fullscreen.chat-history-open .chat-history) {
		display: flex;
		position: fixed;
		top: 0;
		bottom: 0;
		left: 0;
		z-index: 40;
		width: min(300px, 82vw);
		border-radius: 0;
		border-right: 1px solid var(--line);
		box-shadow: var(--shadow);
	}
	.chat-panel {
		display: flex;
		flex-direction: column;
		min-width: 0;
		min-height: 0;
		overflow: hidden;
	}
	.chat-head {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 12px;
		padding: 12px 16px;
		border-bottom: 1px solid var(--line-soft);
		background: var(--chat-head-bg);
		flex-wrap: wrap;
	}
	.chat-head-main {
		display: flex;
		align-items: center;
		gap: 14px;
		min-width: 0;
	}
	.chat-head-label-row {
		display: flex;
		align-items: center;
		gap: 8px;
		min-width: 0;
		margin-bottom: 3px;
	}
	.chat-head-label {
		display: block;
		font-size: 9px;
		letter-spacing: 0.08em;
		text-transform: uppercase;
		color: var(--muted);
		font-weight: 700;
		flex: none;
	}
	.model-select {
		width: min(320px, 46vw);
		padding-top: 6px;
		padding-bottom: 6px;
		font-size: 12px;
		background-color: var(--surface-inset);
	}
	.current-title {
		font-size: 10.5px;
		color: var(--muted);
		max-width: 180px;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.chat-head-right {
		display: flex;
		align-items: center;
		gap: 8px;
		flex-wrap: wrap;
		justify-content: flex-end;
	}
	.privacy-control,
	.websearch-control {
		display: flex;
		align-items: center;
		gap: 7px;
		font-size: 11.5px;
		color: var(--muted);
	}
	.privacy-state {
		width: 22px;
		font-weight: 600;
		color: var(--text-soft);
	}
	.messages {
		flex: 1;
		overflow-y: auto;
		padding: 18px 18px 12px;
		display: flex;
		flex-direction: column;
		gap: 16px;
		min-height: 0;
	}
	.welcome {
		flex: 1;
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		text-align: center;
		gap: 10px;
		min-height: 280px;
		color: var(--muted);
	}
	.welcome-mark {
		width: 68px;
		height: 68px;
		border-radius: 22px;
		display: grid;
		place-items: center;
		color: var(--accent);
		background: var(--accent-soft);
		border: 1px solid var(--accent-border-soft);
		margin-bottom: 6px;
		box-shadow: 0 18px 50px var(--accent-ring);
	}
	.welcome h2 {
		font-size: 22px;
		font-weight: 700;
		letter-spacing: -0.4px;
		color: var(--text);
	}
	.welcome p {
		font-size: 12.5px;
		line-height: 1.6;
		max-width: 400px;
	}
	.message {
		display: flex;
		gap: 9px;
		max-width: 90%;
	}
	.message.user {
		align-self: flex-end;
	}
	.avatar {
		width: 28px;
		height: 28px;
		border-radius: 9px;
		display: grid;
		place-items: center;
		background: var(--panel-3);
		border: 1px solid var(--line);
		color: var(--muted);
		flex: none;
		margin-top: 2px;
	}
	.message.assistant .avatar {
		background: var(--accent-soft);
		color: var(--accent);
		border-color: var(--accent-border);
	}
	.message-body {
		min-width: 0;
	}
	.message-meta {
		display: flex;
		gap: 8px;
		flex-wrap: wrap;
		font-size: 9.5px;
		color: var(--muted);
		margin-bottom: 4px;
		letter-spacing: 0.02em;
	}
	.message.user .message-meta {
		justify-content: flex-end;
	}
	.message-safe {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		padding: 3px;
		color: var(--safe);
		opacity: 0.9;
	}
	.message-meta span:first-child {
		font-weight: 700;
		color: var(--text-soft);
	}
	.message-text {
		font-size: 12.5px;
		line-height: 1.6;
		overflow-wrap: anywhere;
		padding: 9px 12px;
		border-radius: 11px;
		border: 1px solid var(--line-soft);
		background: var(--chat-bubble);
		color: var(--chat-bubble-text);
	}
	.message.user .message-text {
		background: var(--chat-user-bg);
		border-color: var(--chat-user-border);
		color: var(--chat-user-text);
	}
	.message.assistant .message-text {
		border-top-left-radius: 4px;
	}
	.message.user .message-text {
		border-top-right-radius: 4px;
	}
	.message-footer {
		display: flex;
		margin-top: 4px;
	}
	.message.user .message-footer {
		justify-content: flex-end;
	}
	.message-actions {
		display: inline-flex;
		align-items: center;
		gap: 2px;
		opacity: 0.55;
		transition: opacity 0.12s ease;
	}
	.message:hover .message-actions,
	.message:focus-within .message-actions {
		opacity: 1;
	}
	:global(.message-actions button) {
		border: 0;
		background: transparent;
		color: var(--muted);
		display: grid;
		place-items: center;
		padding: 3px;
		border-radius: 4px;
	}
	:global(.message-actions button:hover) {
		background: var(--hover-tint);
		color: var(--text);
	}
	:global(.message-actions button.active) {
		background: var(--accent-soft);
		color: var(--accent);
	}
	.tool-indicator {
		display: grid;
		place-items: center;
		padding: 3px;
		color: var(--accent);
	}
	.revision-inline {
		display: inline-flex;
		align-items: center;
		gap: 3px;
		padding: 2px 4px;
		border: 1px solid var(--line);
		border-radius: 6px;
		color: var(--warning);
		background: var(--warning-soft);
		font-size: 10px;
		font-variant-numeric: tabular-nums;
	}
	.revision-inline button {
		border: 0;
		background: transparent;
		color: var(--warning);
		display: grid;
		place-items: center;
		padding: 1px;
	}
	.revision-inline button:disabled {
		opacity: 0.3;
		cursor: not-allowed;
	}
	.message-editor {
		display: flex;
		flex-direction: column;
		gap: 7px;
	}
	.edit-input {
		width: min(620px, 72vw);
		min-height: 92px;
		resize: vertical;
		border: 1px solid var(--line);
		border-radius: 10px;
		background: var(--surface-inset);
		color: var(--text);
		padding: 10px 12px;
		outline: none;
		font-size: 13px;
		line-height: 1.55;
	}
	.edit-input:focus {
		border-color: var(--accent-border-strong);
		box-shadow: 0 0 0 3px var(--accent-ring);
	}
	.edit-hint {
		font-size: 10px;
		color: var(--muted);
	}
	.edit-actions {
		display: flex;
		justify-content: flex-end;
		gap: 6px;
	}
	.markdown :global(p) {
		margin: 0 0 0.7em;
	}
	.markdown :global(p:last-child) {
		margin-bottom: 0;
	}
	.markdown :global(h1),
	.markdown :global(h2),
	.markdown :global(h3),
	.markdown :global(h4) {
		margin: 0.9em 0 0.45em;
		line-height: 1.3;
		color: var(--text);
	}
	.markdown :global(h1:first-child),
	.markdown :global(h2:first-child),
	.markdown :global(h3:first-child),
	.markdown :global(h4:first-child) {
		margin-top: 0;
	}
	.markdown :global(h1) {
		font-size: 1.45em;
	}
	.markdown :global(h2) {
		font-size: 1.28em;
	}
	.markdown :global(h3) {
		font-size: 1.15em;
	}
	.markdown :global(ul),
	.markdown :global(ol) {
		margin: 0.45em 0 0.8em;
		padding-left: 1.5em;
	}
	.markdown :global(li) {
		margin: 0.18em 0;
	}
	.markdown :global(blockquote) {
		margin: 0.6em 0;
		padding: 0.45em 0.8em;
		border-left: 3px solid var(--accent);
		background: var(--accent-soft);
		border-radius: 0 6px 6px 0;
		color: var(--text-soft);
	}
	.markdown :global(code) {
		font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
		font-size: 0.9em;
		background: var(--surface-code);
		border: 1px solid var(--line-soft);
		border-radius: 4px;
		padding: 0.1em 0.35em;
	}
	.markdown :global(pre) {
		margin: 0.65em 0;
		padding: 11px 12px;
		background: var(--surface-code);
		border: 1px solid var(--line-soft);
		border-radius: 8px;
		overflow-x: auto;
	}
	.markdown :global(pre code) {
		background: transparent;
		border: 0;
		padding: 0;
		font-size: 11.5px;
		line-height: 1.6;
	}
	.markdown :global(a) {
		color: var(--accent);
		text-decoration: underline;
		text-underline-offset: 2px;
	}
	.markdown :global(table) {
		border-collapse: collapse;
		margin: 0.65em 0;
		font-size: 11.5px;
		display: block;
		overflow-x: auto;
	}
	.markdown :global(th),
	.markdown :global(td) {
		border: 1px solid var(--line);
		padding: 5px 8px;
		text-align: left;
	}
	.markdown :global(th) {
		background: var(--surface-inset);
	}
	.markdown :global(hr) {
		border: 0;
		border-top: 1px solid var(--line);
		margin: 1em 0;
	}
	.markdown :global(img) {
		max-width: 100%;
		border-radius: 7px;
	}
	.stream-cursor {
		display: inline-block;
		width: 6px;
		height: 14px;
		margin-left: 2px;
		vertical-align: -2px;
		border-radius: 1px;
		background: var(--accent);
		animation: stream-blink 1s steps(2, start) infinite;
	}
	@keyframes stream-blink {
		50% {
			opacity: 0;
		}
	}
	.message-images {
		display: flex;
		gap: 7px;
		flex-wrap: wrap;
		margin-bottom: 7px;
		justify-content: flex-end;
	}
	.message.assistant .message-images {
		justify-content: flex-start;
	}
	.message-images img {
		width: 132px;
		height: 100px;
		object-fit: cover;
		border-radius: 9px;
		border: 1px solid var(--line);
	}
	.reasoning-block {
		margin-bottom: 7px;
		padding: 8px 10px;
		border: 1px solid var(--line-soft);
		border-radius: 9px;
		background: var(--surface-inset);
	}
	.reasoning-text {
		max-height: 260px;
		overflow-y: auto;
		white-space: pre-wrap;
		overflow-wrap: anywhere;
		font-size: 11.5px;
		line-height: 1.55;
		color: var(--muted);
	}
	.thinking {
		display: flex;
		align-items: center;
		gap: 9px;
		padding: 10px 13px;
		border: 1px solid var(--line-soft);
		border-radius: 12px;
		color: var(--muted);
		font-size: 12px;
		background: var(--chat-bubble);
	}
	.chat-error {
		display: flex;
		align-items: center;
		gap: 8px;
		margin: 0 18px 8px;
		padding: 9px 11px;
		border: 1px solid rgba(251, 113, 133, 0.25);
		background: var(--danger-soft);
		color: var(--danger-text);
		border-radius: 9px;
		font-size: 11.5px;
	}
	.chat-error span {
		flex: 1;
	}
	.composer {
		padding: 10px 14px 12px;
		border-top: 1px solid var(--line-soft);
		background: var(--chat-composer-bg);
	}
	.composer-box {
		display: flex;
		align-items: flex-end;
		gap: 6px;
		border: 1px solid var(--line);
		background: var(--surface-inset);
		border-radius: 12px;
		padding: 6px;
		transition:
			border-color 0.15s ease,
			box-shadow 0.15s ease;
	}
	.composer-box:focus-within {
		border-color: var(--accent-border-strong);
		box-shadow: 0 0 0 3px var(--accent-ring);
	}
	.attach-btn {
		margin-bottom: 1px;
		color: var(--muted);
	}
	.chat-input {
		flex: 1;
		resize: none;
		border: 0;
		background: transparent;
		outline: 0;
		color: var(--text);
		font-size: 13px;
		line-height: 1.55;
		padding: 8px 3px;
		max-height: 180px;
		min-height: 34px;
	}
	.chat-input::placeholder {
		color: var(--placeholder);
	}
	.send-btn,
	.stop-btn {
		width: 34px;
		height: 34px;
		padding: 0;
		border-radius: 9px;
		flex: none;
	}
	.composer-hint {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 12px;
		flex-wrap: wrap;
		color: var(--muted);
		font-size: 10px;
		padding: 6px 4px 0;
	}
	.privacy-note {
		display: inline-flex;
		align-items: center;
		gap: 4px;
		color: var(--warning);
	}
	.attachment-strip {
		display: flex;
		gap: 8px;
		padding: 0 2px 9px;
		flex-wrap: wrap;
	}
	.attachment {
		position: relative;
	}
	.attachment img {
		width: 54px;
		height: 54px;
		object-fit: cover;
		border-radius: 8px;
		border: 1px solid var(--line);
	}
	.attachment-remove {
		position: absolute;
		top: -5px;
		right: -5px;
		width: 17px;
		height: 17px;
		border-radius: 50%;
		border: 1px solid var(--line);
		background: var(--attachment-remove-bg);
		color: var(--text-soft);
		display: grid;
		place-items: center;
		padding: 0;
	}
	.attachment-remove:hover {
		background: var(--danger);
		color: var(--accent-contrast);
	}
	.chat-empty {
		flex: 1;
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		text-align: center;
		gap: 10px;
		padding: 32px;
	}
	@media (max-width: 980px) {
		.chat-layout {
			grid-template-columns: 210px minmax(0, 1fr);
		}
		.current-title {
			display: none;
		}
	}
	@media (max-width: 860px) {
		.chat-layout {
			grid-template-columns: 1fr;
			grid-template-rows: auto minmax(0, 1fr);
			height: calc(100dvh - 160px);
			min-height: 0;
		}
		.chat-history {
			max-height: 118px;
		}
		.chat-panel {
			height: auto;
			min-height: 0;
		}
		.message {
			max-width: 94%;
		}
		.message-actions {
			opacity: 1;
		}
		.edit-input {
			width: 100%;
		}
	}
</style>
