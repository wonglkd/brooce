package tpl


// <form action="" method="post">
//   <input type="hidden" name="csrf" value="{{CSRF}}">
//   <input type="hidden" name="item" value="{{.Raw}}">

//   {{ if eq $.ListType "failed" "delayed" "done" }}
//   <button type="submit" formaction="{{BasePath}}/retry/{{ $.ListType }}/{{ $.QueueName }}" class="btn btn-warning btn-xs">
//     <span class="glyphicon glyphicon-repeat"></span>
//     Retry
//   </button>
//   {{ end }}
// </form>

var showLogTpl = `
{{ define "showlog" }}
{{ template "header" "" }}



<div class="row">
  <div class="col-md-12">
    <pre>{{.}}</pre>
  </div>
</div>
<script>
document.body.onload = function() { $('pre').scrollTop = $('pre').scrollHeight; }
</script>
{{ template "footer" }}
{{ end }}
`
