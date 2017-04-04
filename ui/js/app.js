"use strict";

$(function(){
    var guid = function(sep) {
        if(sep === undefined){
            sep = '-';
        }

        function s4() {
            return Math.floor((1 + Math.random()) * 0x10000).toString(16).substring(1);
        }

        return s4() + s4() + sep + s4() + sep + s4() + sep + s4() + sep + s4() + s4() + s4();
    };

    var Byteflood = Stapes.subclass({
        constructor: function(){
            // prevent normal form submissions, we'll handle them here
            $('form').on('submit', function(e){
                this.submitForm(e);
                e.preventDefault();
            }.bind(this));

            // setup typeahead for fields that have it
            $('.typeahead').typeahead({
                highlight: true,
                async: true,
            },{
                limit: 9,
                source: function(query, _, asyncResults){
                    var url = $('.typeahead').data('typeahead-url');
                    url = url.replace('{}', query.replace(/^\//, ''));

                    console.log(url);
                    if(url){
                        $.ajax(url, {
                            success: function(data){
                                asyncResults(data);
                            }.bind(this),
                            error: this.showResponseError.bind(this),
                        });
                    }
                }.bind(this),
            });

            this.setupPartials();
        },

        setupPartials: function(){
            this._partials = {};

            $('[bf-load]').each(function(i, element){
                element = $(element);
                var id = element.attr('id');

                if(!id){
                    id = 'bf_' + guid('');
                    element.attr('id', id);
                }

                // setup partial from element
                if(!this._partials[id]){
                    var partial = new Partial(
                        id,
                        element,
                        element.attr('bf-load'), {
                            'interval': element.attr('bf-interval'),
                            'onload': element.attr('bf-onload'),
                        });

                    // load the partial and, if an interval is given, start a timer to
                    // periodically reload
                    partial.init();

                    this._partials[id] = partial;
                }

            }.bind(this));
        },

        loadInto: function(selector, url, payload) {
            $.ajax(url, {
                method: 'GET',
                data: payload,
                success: function(data){
                    $(selector).replaceWith(data);
                }.bind(this),
                error: this.showResponseError.bind(this),
            });
        },

        notify: function(message, type, details, config){
            $.notify($.extend(details, {
                'message': message,
            }), $.extend(config, {
                'type': (type || 'info'),
            }));
        },

        queueFileForDownload: function(session, share_id, file_id) {
            $.ajax('/api/downloads/'+session+'/'+share_id+'/'+file_id, {
                method: 'POST',
                success: function(){
                    this.notify('File '+file_id+' has been queued for download.');
                }.bind(this),
                error: this.showResponseError.bind(this),
            });
        },

        scan: function() {
            var scanRequest = '';

            if(arguments.length){
                scanRequest = JSON.stringify({
                    'labels': $.makeArray(arguments),
                });
            }

            $.ajax('/api/db/actions/scan?force=true', {
                method: 'POST',
                data: scanRequest,
                success: function(){
                    location.reload();
                }.bind(this),
                error: this.showResponseError.bind(this),
            });
        },

        performAction: function(path, callback) {
            $.ajax('/api/'+path, {
                method: 'POST',
                success: function(){
                    if(callback){
                        callback.bind(this)();
                    }
                }.bind(this),
                error: this.showResponseError.bind(this),
            });
        },

        delete: function(model, id, callback) {
            if(confirm("Are you sure you want to remove this item?") === true){
                $.ajax('/api/'+model+'/'+id.toString(), {
                    method: 'DELETE',
                    success: function(){
                        if(callback){
                            callback.bind(this)();
                        }else{
                            location.reload();
                        }
                    }.bind(this),
                    error: this.showResponseError.bind(this),
                });
            }
        },

        submitForm: function(event){
            var form = $(event.target);
            var url = '';

            if(form.action && form.action.length > 0){
                url = form.action;
            }else if(name = form.attr('name')){
                url = '/api/' + name;
            }else{
                this.notify('Could not determine path to submit data to', 'error');
                return;
            }

            var createNew = true;
            var record = {
                'fields': {},
            };

            $.each(form.serializeArray(), function(i, field) {
                // if(field.value == '' || field.value == '0'){
                //     delete field['value'];
                // }

                if(field.name == "id"){
                    if(field.value){
                        createNew = false;
                    }

                    record['id'] = field.value;
                }else if(field.value !== undefined){
                    record['fields'][field.name] = field.value;
                }
            });

            $.ajax(url, {
                method: (form.attr('method') || (createNew ? 'POST' : 'PUT')),
                data: JSON.stringify({
                    'records': [record],
                }),
                success: function(){
                    var redirectTo = (form.data('redirect-to') || '/'+form.attr('name'));
                    location.href = redirectTo;
                }.bind(this),
                error: this.showResponseError.bind(this),
            })
        },

        showResponseError: function(response){
            this.notify(response.responseText, 'danger', {
                'icon': 'fa fa-warning',
                'title': '<b>' +
                    response.statusText + ' (HTTP '+response.status.toString()+')' +
                    '<br />' +
                '</b>',
            });
        },
    });

    var Partial = Stapes.subclass({
        constructor: function(id, element, url, options){
            this.id = id;
            this.element = element;
            this.url = url;
            this.options = (options || {});
        },

        init: function(){
            // console.debug('Initializing partial', '#'+this.id, this.options);

            this.load();

            // this is a no-op if autoreloading isn't requested
            this.monitor();
        },

        clear: function(){
            this.element.empty();
        },

        load: function(){
            if(this.url) {
                $(this.element).load(this.url, null, function(response, status, xhr){
                    if(xhr.status < 400){
                        if(this.options.onload){
                            eval(this.options.onload);
                        }
                    }else{
                        if(this.options.onerror){
                            eval(this.options.onerror);
                        }else{
                            this.clear();
                        }
                    }
                }.bind(this));
            }
        },

        monitor: function(){
            // setup the interval if it exists and is <= 60 updates/sec.
            if(this.options.interval > 8 && !this._interval){
                this._interval = window.setInterval(this.load.bind(this), this.options.interval);
            }
        },

        stop: function(){
            if(this._interval){
                window.clearInterval(this._interval);
            }
        },
    });

    window.byteflood = new Byteflood();
});
