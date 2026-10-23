/* Credential-free artifact client. All live authority belongs to the outer shell. */
const $=id=>document.getElementById(id);
let state,live,connected=false,drafts={},sceneRendered='',pendingMessage,recognitionRendered='',editingTranscript,pendingTranscript;
const post=data=>{if(window.top!==window)window.top.postMessage({channel:'vox-studio',...data},'*');};
const esc=s=>String(s??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
const command=(type,values={})=>{const id=crypto.randomUUID();post({type:'command',command:{id,type,...values}});return id;};
function draft(key,value){drafts[key]=value;post({type:'draft',key,value});}
function restore(){for(const el of document.querySelectorAll('[data-draft]'))if(drafts[el.dataset.draft]!==undefined)el.value=drafts[el.dataset.draft];}
document.addEventListener('input',e=>{if(e.target.dataset.draft)draft(e.target.dataset.draft,e.target.value);});
function controls(){
  const enabled=connected&&!state?.ended;
  for(const id of ['start-voice','stop-voice','cancel-work','end-session','send','reconnect','add-scene'])$(id).disabled=!enabled;
  $('start-voice').disabled=!enabled||live?.voice;
  $('stop-voice').disabled=!enabled||!live?.voice;
  const changingRecognition=live?.liveRecognitionSwitch?!!live.recognitionPending:live?.recognitionBusy;
  for(const id of ['recognition-profile','recognition-pause','recognition-save'])$(id).disabled=!enabled||changingRecognition;
  $('connection').textContent=state?.ended?'Session ended':connected?'Connected':'Offline review';$('connection').classList.toggle('online',connected&&!state?.ended);
}
window.addEventListener('message',e=>{
  if(e.source!==window.top||e.data?.channel!=='vox-studio')return;
  const d=e.data;
  if(d.type==='drafts'){drafts=d.drafts||{};restore();}
  if(d.type==='connection'){connected=d.connected;controls();}
  if(d.type==='state'){state=d.state;live=d.live;render();controls();}
  if(d.type==='error')$('notice').textContent=d.error;
  if(d.type==='ack'){if(d.id===pendingMessage?.id){if($('message').value===pendingMessage.text){$('message').value='';draft('message','');}pendingMessage=undefined;}$('notice').textContent='';}
  if(d.type==='ack'&&d.id===pendingTranscript){draft('transcript:'+editingTranscript,'');editingTranscript=undefined;pendingTranscript=undefined;render();}
  if(d.type==='error'&&d.id===pendingTranscript)pendingTranscript=undefined;
});
$('start-voice').onclick=()=>command('voice.start');$('stop-voice').onclick=()=>command('voice.stop');
$('cancel-work').onclick=()=>command('work.cancel');$('end-session').onclick=()=>command('session.end');
$('reconnect').onclick=()=>command('feedback.reconnect');
$('composer').onsubmit=e=>{e.preventDefault();if(!connected||!$('message').value.trim()||pendingMessage)return;const text=$('message').value;pendingMessage={id:command('message',{text}),text};};
$('add-scene').onclick=()=>command('scene.add',{title:`Scene ${(state?.scenes.length||0)+1}`});
$('recognition-form').onsubmit=e=>{e.preventDefault();const [engine,model]=$('recognition-profile').value.split(':');command('voice.configure',{settings:{engine,model,language:'en',silenceMs:Math.round(Number($('recognition-pause').value)*1000)}});};
function renderRecognition(){
  const r=state.recognition;if(!r)return;
  $('recognition-active').textContent=`${r.engine} · ${r.model} · ${r.language==='en'?'English':'Auto language'}`;
  const status=$('recognition-status');
  if(status)status.textContent=live.recognitionPending?`${live.recognitionSwitching?'Loading':'Waiting for the current utterance and reply to finish before loading'} ${live.recognitionPending.model}…`:live.recognitionError||(!live.liveRecognitionSwitch?'Live switching will be available after the next safe runtime restart.':'Choose a model and apply it while voice is on. Listening pauses briefly while the model loads.');
  const key=JSON.stringify(r);
  if(key!==recognitionRendered){recognitionRendered=key;$('recognition-profile').value=`${r.engine}:${r.model}`;$('recognition-pause').value=r.silenceMs/1000;restore();}
  const last=state.recordings.findLast(r=>r.recognition)?.recognition;
  $('recognition-timing').textContent=last?`Last transcription: ${(last.durationMs/1000).toFixed(1)} seconds · ${last.model}`:'No transcription timing recorded yet.';
}
function renderTokenUsage(){
  const meter=$('token-usage');if(!meter)return;
  const d=meter.dataset;
  const snapshot=d.threadId?{threadId:d.threadId,totalTokens:Number(d.totalTokens),contextTokens:Number(d.contextTokens),contextWindow:d.contextWindow?Number(d.contextWindow):null,updatedAt:d.updatedAt}:undefined;
  const reported=state?.tokenUsage?.threadId===state?.threadId?state?.tokenUsage:undefined;
  const usage=reported||(snapshot?.threadId===state?.threadId?snapshot:undefined);
  const valid=n=>Number.isSafeInteger(n)&&n>=0;
  if(!usage||!valid(usage.totalTokens)||!valid(usage.contextTokens)){
    $('token-total').textContent='—';$('token-context').textContent='—';
    $('token-status').textContent='Usage unavailable';meter.title='Codex has not reported token usage for this conversation.';
    return;
  }
  const format=n=>n.toLocaleString('en-US');
  const hasLimit=valid(usage.contextWindow)&&usage.contextWindow>0;
  $('token-total').textContent=format(usage.totalTokens);
  $('token-context').textContent=hasLimit?`${(100*usage.contextTokens/usage.contextWindow).toFixed(1)}%`:'—';
  const time=new Date(usage.updatedAt);
  $('token-status').textContent=reported?'Reported by Codex':'Saved snapshot';
  meter.title=`Total input and output tokens across this conversation, including reused input. Context: ${format(usage.contextTokens)}${hasLimit?` of ${format(usage.contextWindow)} tokens`:' tokens; limit unavailable'}.${Number.isNaN(time.getTime())?'':` Updated ${time.toLocaleString()}.`}`;
}
function render(){
  renderTokenUsage();renderRecognition();
  for(const id of ['mic','transcription','agent','speech','feedback'])$(id).textContent=live[id]+(id==='agent'&&live.activity?` · ${live.activity}`:'');
  $('thread').textContent=(state.threadId||'connecting').slice(0,12);$('thread').title=state.threadId||'';
  $('saved').textContent=state.ended?'Session ended. Your project and recordings are saved.':`Saved ${new Date(state.updatedAt).toLocaleTimeString()}`;
  const worker=state.agents?.settings,capability=worker?state.agents.capabilities.findLast(c=>c.provider===worker.workerProvider&&c.model===worker.workerModel):undefined;
  $('capabilities').textContent=worker?(capability?`${worker.workerProvider} image capability: ${capability.imageGeneration}. ${capability.detail}`:`${worker.workerProvider} image capability will be checked by the selected worker. Follow progress in Agents.`):state.capabilities.detail;
  $('queue').textContent=`${state.actions.filter(a=>a.status==='queued').length} queued · ${state.recordings.length} recordings`;
  const chat=$('conversation'),bottom=chat.scrollHeight-chat.scrollTop-chat.clientHeight<65;
  const html=state.messages.map(m=>`<article class="message ${esc(m.role)}"><span class="who">${esc(m.role==='assistant'?'Codex':m.role==='user'?'You':'Workspace')} ${esc(m.source||'')}</span><p>${esc(m.text)}</p>${m.original?'<small>Edited transcript · original retained</small>':''}${m.role==='user'?`<button type="button" class="small quiet" data-edit-transcript="${esc(m.id)}">Correct transcript</button>`:''}</article>`).join('');
  if(!editingTranscript&&html&&chat.innerHTML!==html){chat.innerHTML=html;if(bottom)chat.scrollTop=chat.scrollHeight;}
  $('streaming').hidden=!live.streaming;$('streaming').textContent=live.streaming;
  const production=state.production,progress=production?[
    state.ended?'Saved':connected?'Connected':'Reconnecting',
    `${state.scenes.length} scenes`,
    production.jobs.some(j=>j.kind==='concept'&&['queued','running'].includes(j.status))?'Generating concept':`${state.scenes.reduce((n,s)=>n+s.revisions.length,0)} concept revisions`,
    `${production.compositions.filter(c=>!c.stale).length} current animations`,
    production.deliveries.length?`${production.deliveries.length} exports`:production.assemblies.some(a=>!a.stale)?'Assembly ready for review':'Build timeline'
  ]:undefined;
  $('roadmap').innerHTML=state.roadmap.map((p,i)=>`<li><b>0${i+1}</b><div>${esc(p.title)}<small>${esc(progress?.[i]??p.status)}</small></div></li>`).join('');
  $('recordings').innerHTML=state.recordings.slice(-15).map(r=>`<p>${esc(new Date(r.at).toLocaleTimeString())} · ${esc(r.status)}<br>${esc(r.text||r.error||'WAV saved locally')}${r.recognition?`<br>${esc(r.recognition.model)} · ${(r.recognition.durationMs/1000).toFixed(1)}s`:''}</p>`).join('');
  $('actions').innerHTML=state.actions.slice(-5).map(a=>`<p>${esc(a.status)} · ${esc(a.text.slice(0,120))}${a.error?'<br>'+esc(a.error):''}</p>`).join('');
  renderRequests();renderScenes();
}
$('conversation').addEventListener('click',e=>{const id=e.target.closest('[data-edit-transcript]')?.dataset.editTranscript;if(!id||editingTranscript)return;const m=state.messages.find(m=>m.id===id);if(!m)return;editingTranscript=id;const form=document.createElement('form');form.innerHTML=`<label>Corrected transcript<textarea data-draft="transcript:${esc(id)}" rows="4">${esc(m.text)}</textarea></label><p class="hint">Saves a correction; previous work is not repeated.</p><button>Save correction</button><button type="button" data-cancel-edit>Cancel</button>`;e.target.closest('article').append(form);restore();form.onsubmit=event=>{event.preventDefault();if(!pendingTranscript)pendingTranscript=command('transcript.edit',{messageId:id,text:form.querySelector('textarea').value});};form.querySelector('[data-cancel-edit]').onclick=()=>{editingTranscript=undefined;render();};});
let requestsKey='';
function renderRequests(){
  const key=JSON.stringify(live.requests);if(key===requestsKey)return;requestsKey=key;
  $('requests').replaceChildren();
  for(const r of live.requests){
    const box=document.createElement('div');box.className='request';
    const heading=document.createElement('h3');heading.textContent='Codex needs your input';box.append(heading);
    if(r.method.endsWith('/requestApproval')){
      const detail=document.createElement('pre');detail.textContent=[r.params.reason,r.params.command,r.params.cwd,r.params.grantRoot,r.params.permissions?JSON.stringify(r.params.permissions,null,2):''].filter(Boolean).join('\n');box.append(detail);
      for(const [label,decision]of [[r.method==='item/permissions/requestApproval'?'Allow for this turn':'Allow once','accept'],['Decline','decline']]){const b=document.createElement('button');b.textContent=label;b.onclick=()=>command('approval',{requestId:r.id,value:{decision}});box.append(b);}
    }else if(r.method==='item/tool/requestUserInput'){
      const form=document.createElement('form');
      for(const q of r.params.questions){const label=document.createElement('label');label.textContent=q.question;const input=document.createElement('input');input.name=q.id;input.required=true;input.placeholder=q.options?.map(o=>o.label).join(' / ')||'Your answer';label.append(input);form.append(label);}
      const b=document.createElement('button');b.textContent='Send answers';form.append(b);form.onsubmit=e=>{e.preventDefault();const answers={};for(const [id,answer]of new FormData(form))answers[id]={answers:[answer]};command('approval',{requestId:r.id,value:{answers}});};box.append(form);
    }else{const p=document.createElement('p');p.textContent=`Unsupported request: ${r.method}. Cancel work to stop it.`;box.append(p);}
    $('requests').append(box);
  }
}
function renderScenes(){
  $('scene-list').innerHTML=state.scenes.map(s=>`<button data-scene="${esc(s.id)}" class="${s.id===state.selectedScene?'selected':''}">${esc(s.title)}</button>`).join('');
  for(const b of $('scene-list').querySelectorAll('button'))b.onclick=()=>command('scene.select',{sceneId:b.dataset.scene});
  const scene=state.scenes.find(s=>s.id===state.selectedScene);if(!scene)return;
  const key=`${scene.id}:${scene.version}`;if(sceneRendered===key)return;sceneRendered=key;
  $('scene-panel').innerHTML=`<form id="scene-form"><div class="scene-fields">${[['title','Scene title'],['purpose','Purpose'],['elements','Elements'],['composition','Composition'],['narration','Proposed narration']].map(([key,label])=>`<label class="${key==='narration'?'wide':''}">${label}<textarea name="${key}" data-draft="${scene.id}:${key}" rows="2">${esc(scene[key])}</textarea></label>`).join('')}</div><p><button>Save scene notes</button></p></form><div class="references">${scene.references.map(r=>`<p>${/^https?:/.test(r.value)?`<a href="${esc(r.value)}" target="_blank" rel="noreferrer">${esc(r.value)}</a>`:esc(r.value)}<br><small>${esc(r.note)}${r.timestamp===undefined?'':` · ${r.timestamp}s`}</small></p>`).join('')}</div><form id="reference-form"><label>Reference link or local file<input name="value" data-draft="${scene.id}:reference" required placeholder="https://… or /path/to/file"></label><label>Notes<input name="note" data-draft="${scene.id}:reference-note"></label><label>Timestamp (seconds, optional)<input name="timestamp" type="number" min="0" step="0.1"></label><p><button>Add reference</button></p></form>`;
  restore();
  $('scene-form').onsubmit=e=>{e.preventDefault();const values=Object.fromEntries(new FormData(e.currentTarget));command('scene.update',{sceneId:scene.id,version:scene.version,values});};
  $('reference-form').onsubmit=e=>{e.preventDefault();const v=Object.fromEntries(new FormData(e.currentTarget));command('reference.add',{sceneId:scene.id,value:v.value,note:v.note,...(v.timestamp?{timestamp:Number(v.timestamp)}:{})});};
}
post({type:'ready'});
// Reloaded artifacts request the latest snapshot; recordings and turns live outside this frame.
setInterval(()=>{if(!state)post({type:'ready'});},1500);
