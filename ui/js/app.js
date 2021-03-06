"use strict";

$(function(){
    var originalSerializeArray = $.fn.serializeArray;
    $.fn.extend({
        serializeArray: function () {
            var brokenSerialization = originalSerializeArray.apply(this);
            var checkboxValues = $(this).find('input[type=checkbox]').map(function () {
                return { 'name': this.name, 'value': this.checked };
            }).get();
            var checkboxKeys = $.map(checkboxValues, function (element) { return element.name; });
            var withoutCheckboxes = $.grep(brokenSerialization, function (element) {
                return $.inArray(element.name, checkboxKeys) == -1;
            });

            return $.merge(withoutCheckboxes, checkboxValues);
        }
    });

    window.guid = function(sep) {
        if(sep === undefined){
            sep = '-';
        }

        function s4() {
            return Math.floor((1 + Math.random()) * 0x10000).toString(16).substring(1);
        }

        return s4() + s4() + sep + s4() + sep + s4() + sep + s4() + sep + s4() + s4() + s4();
    };

    window.hexToRGB = function(hex, alpha) {
        hex = hex.replace(/^#/, '');

        var r = parseInt(hex.slice(0, 2), 16),
            g = parseInt(hex.slice(2, 4), 16),
            b = parseInt(hex.slice(4, 6), 16);

        if(alpha){
            return "rgba(" + r + ", " + g + ", " + b + ", " + alpha + ")";
        } else {
            return "rgb(" + r + ", " + g + ", " + b + ")";
        }
    }

    var Byteflood = Stapes.subclass({
        constructor: function(){
            window.location.url = new Url;

            // prevent normal form submissions, we'll handle them here
            $('form').on('submit', function(e){
                this.submitForm(e);
                e.preventDefault();
            }.bind(this));

            // setup typeahead for fields that have it
            $('.typeahead').each(function(i, el){
                el = $(el);

                el.typeahead({
                    highlight: true,
                    async: true,
                },{
                    limit: parseInt(el.data('typeahead-limit') || 9),
                    source: function(query, _, asyncResults){
                        var url = el.data('typeahead-url');

                        if(url){
                            url = url.replace('{}', query.replace(/^\//, ''));

                            $.ajax(url, {
                                success: function(data){
                                    asyncResults(data);
                                }.bind(this),
                                error: this.showResponseError.bind(this),
                            });
                        }
                    }.bind(this),
                });
            }.bind(this))

            $('.combobox').combobox();

            this._partials = {};
            this.setupPartials();
        },

        setupPartials: function(){
            $('[bf-load]').each(function(i, element){
                element = $(element);
                var id = element.attr('id');

                if(!id){
                    id = 'bf_' + guid('');
                    element.attr('id', id);
                }

                // setup partial from element
                if(!this._partials[id]){
                    var params = {};

                    $.each(element[0].attributes, function(i, attr){
                        if(attr.name.match(/^bf-param-/)){
                            params[attr.name.replace(/^bf-param-/, '')] = attr.value;
                        }
                    });

                    var partial = new Partial(
                        id,
                        element,
                        element.attr('bf-load'), {
                            'interval': element.attr('bf-interval'),
                            'onload': element.attr('bf-onload'),
                            'replace': (element.attr('bf-replace') === 'true'),
                            'params': params,
                        });

                    // load the partial and, if an interval is given, start a timer to
                    // periodically reload
                    partial.init();

                    this._partials[id] = partial;
                }

            }.bind(this));
        },

        loadInto: function(selector, url, config) {
            return this.loadElement(selector, url, config, false);
        },

        replaceWith: function(selector, url, config, onError) {
            return this.loadElement(selector, url, config, true);
        },

        loadElement: function(selector, url, config, replace) {
            var onError, onSuccess, params;

            if($.isPlainObject(config)){
                onSuccess = config['success'];
                onError = config['error'];
                params = config['params'];
            }

            $.ajax(url, {
                method: 'GET',
                data: params,
                success: function(data){
                    var el = $(selector);

                    if(replace === true){
                        el.replaceWith(data);
                    }else{
                        el.html(data);
                    }

                    this.setupPartials();

                    if($.isFunction(onSuccess)){
                        onSuccess(data, el);
                    }
                }.bind(this),
                error: function(response){
                    if(onError === false){
                        return;
                    }else if($.isFunction(onError)){
                        return onError.bind(this)(response);
                    }else{
                        return this.showResponseError.bind(this)(response);
                    }
                }.bind(this),
            });
        },

        handleMultiClick: function(event, actions) {
            console.log(event, actions, actionName)

            if($.isPlainObject(actions)) {
                var actionName = '';

                switch(event.button){
                case 0:
                    actionName = 'left';
                    break;;
                case 1:
                    actionName = 'middle';
                    break;;
                case 2:
                    actionName = 'right';
                    break;;
                case 3:
                    actionName = 'back';
                    break;;
                case 4:
                    actionName = 'right';
                    break;;
                }

                if($.isFunction(actions[actionName])){
                    actions[actionName](event, actionName);
                    event.preventDefault();
                }else if($.isFunction(actions['default'])){
                    actions['default'](event, 'default');
                    event.preventDefault();
                }
            }
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

        cleanup: function(callback){
            this.performAction('db/actions/cleanup', callback);
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

        updateIfEmpty: function(target_field, value) {
            if(target_field.value.length == 0){
                target_field.value = value;
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

                console.log(field.name, field.value)

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

        queryMetrics: function(name, options, callback) {
            options = (options || {});
            var url = '/api/metrics/query/'+name+'?';
            var qs = [];

            if(options['interval']){
                qs.push('interval='+options['interval']);
            }

            if(options['from']){
                qs.push('from='+options['from']);
            }

            if(options['group']){
                qs.push('group='+options['group']);
            }

            if(options['fn']){
                qs.push('fn='+options['fn']);
            }

            qs.push('palette='+(options['palette'] || 'spectrum14'));


            $.ajax(url+qs.join('&'), {
                success: function(metrics){
                    if($.isFunction(callback)){
                        callback.bind(this)(metrics);
                    }
                }.bind(this),
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
                $.ajax(this.url, {
                    method:  (this.options.method || 'get'),
                    data:    this.options.params,
                    success: function(body, status, xhr){
                        var el = $(this.element);

                        if(xhr.status < 400){
                            if(this.options.onload){
                                eval(this.options.onload);
                            }

                            if(this.options.replace === true){
                                el.replaceWith(body);
                            }else{
                                el.html(body)
                            }
                        }else{
                            if(this.options.onerror){
                                eval(this.options.onerror);
                            }else{
                                this.clear();
                            }
                        }
                    }.bind(this),
                });
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
